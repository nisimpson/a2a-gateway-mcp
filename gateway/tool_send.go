package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	taskPollInterval = 2 * time.Second
	taskPollTimeout  = 60 * time.Second
)

// handleSendMessage sends a text message to a connected A2A agent by alias or URL.
// Requirement: AGMCP-2.1, AGMCP-2.2, AGMCP-2.5, AGMCP-2.6, AGMCP-2.7 — message send with context lifecycle
func (s *Server) handleSendMessage(ctx context.Context, _ *mcp.CallToolRequest, input SendMessageInput) (*mcp.CallToolResult, any, error) {
	// Validate agent identifier is provided.
	if input.Agent == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "agent identifier is required"}},
		}, nil, nil
	}

	// Validate message.
	if err := ValidateMessage(input.Message); err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}

	// Resolve agent identifier (alias or URL).
	resolved, err := ResolveAgent(s.registry, input.Agent)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}

	// Determine context_id priority: explicit > stored > empty (new conversation).
	contextID := input.ContextID
	if contextID == "" && resolved.IsAlias {
		contextID = s.contextStore.Get(input.Agent)
	}

	// Build A2A message with TextPart content.
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(input.Message))
	if contextID != "" {
		msg.ContextID = contextID
	}

	// Build SendMessageRequest.
	sendReq := &a2a.SendMessageRequest{
		Message: msg,
	}

	// Serialize the request body.
	reqBody, err := json.Marshal(sendReq)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to serialize request: %v", err)}},
		}, nil, nil
	}

	// Determine which HTTP client to use.
	client := s.httpClient
	if resolved.IsAlias {
		entry := s.registry.Lookup(input.Agent)
		if entry != nil {
			client = httpClientForAgent(s.httpClient, entry)
		}
	}

	// Send HTTP POST to the agent URL.
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, resolved.URL, bytes.NewReader(reqBody))
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to create request: %v", err)}},
		}, nil, nil
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("agent unreachable: %v", err)}},
		}, nil, nil
	}
	defer resp.Body.Close()

	// Read response body.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to read response: %v", err)}},
		}, nil, nil
	}

	// Parse the response. The A2A protocol returns either a Task or a Message.
	// Try parsing as a Task first (most common for send_message).
	var task a2a.Task
	if err := json.Unmarshal(body, &task); err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to parse response: %v", err)}},
		}, nil, nil
	}

	// Handle task states.
	switch task.Status.State {
	case a2a.TaskStateCompleted:
		// Update context store with returned context_id (only for alias-based requests).
		if resolved.IsAlias && task.ContextID != "" {
			s.contextStore.Set(input.Agent, task.ContextID)
		}
		return FormatTaskResponse(&task), nil, nil

	case a2a.TaskStateFailed:
		// Update context store even on failure if context_id is present.
		if resolved.IsAlias && task.ContextID != "" {
			s.contextStore.Set(input.Agent, task.ContextID)
		}
		failMsg := "task failed"
		if task.Status.Message != nil {
			text := extractStatusMessageText(task.Status.Message)
			if text != "" {
				failMsg = text
			}
		}
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: failMsg}},
		}, nil, nil

	case a2a.TaskStateCanceled:
		// Update context store even on cancel if context_id is present.
		if resolved.IsAlias && task.ContextID != "" {
			s.contextStore.Set(input.Agent, task.ContextID)
		}
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "task was canceled by the agent"}},
		}, nil, nil

	default:
		// Requirement: AGMCP-2.10 — poll up to 60s for non-terminal states
		task, err := s.pollTaskCompletion(ctx, client, resolved.URL, task.ID)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, nil, nil
		}
		// Re-evaluate the terminal state after polling.
		if resolved.IsAlias && task.ContextID != "" {
			s.contextStore.Set(input.Agent, task.ContextID)
		}
		switch task.Status.State {
		case a2a.TaskStateCompleted:
			return FormatTaskResponse(task), nil, nil
		case a2a.TaskStateFailed:
			failMsg := "task failed"
			if task.Status.Message != nil {
				text := extractStatusMessageText(task.Status.Message)
				if text != "" {
					failMsg = text
				}
			}
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: failMsg}},
			}, nil, nil
		case a2a.TaskStateCanceled:
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "task was canceled by the agent"}},
			}, nil, nil
		default:
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("timeout waiting for task completion (state: %s)", task.Status.State)}},
			}, nil, nil
		}
	}
}

// pollTaskCompletion polls the agent for task status every 2s until a terminal
// state is reached or 60s elapses.
func (s *Server) pollTaskCompletion(ctx context.Context, client *http.Client, agentURL string, taskID a2a.TaskID) (*a2a.Task, error) {
	deadline := time.Now().Add(taskPollTimeout)
	ticker := time.NewTicker(taskPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for task completion: context canceled")
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("timeout waiting for task completion after %s", taskPollTimeout)
			}

			task, err := s.getTaskStatus(ctx, client, agentURL, taskID)
			if err != nil {
				return nil, fmt.Errorf("failed to poll task status: %v", err)
			}

			switch task.Status.State {
			case a2a.TaskStateCompleted, a2a.TaskStateFailed, a2a.TaskStateCanceled:
				return task, nil
			}
			// Still non-terminal, continue polling.
		}
	}
}

// getTaskStatus sends a tasks/get request to retrieve the current task state.
func (s *Server) getTaskStatus(ctx context.Context, client *http.Client, agentURL string, taskID a2a.TaskID) (*a2a.Task, error) {
	getReq := &a2a.GetTaskRequest{ID: taskID}
	reqBody, err := json.Marshal(getReq)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize get task request: %v", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, agentURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("agent unreachable: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	var task a2a.Task
	if err := json.Unmarshal(body, &task); err != nil {
		return nil, fmt.Errorf("failed to parse task response: %v", err)
	}

	return &task, nil
}

// extractStatusMessageText extracts text from a task status message's parts.
func extractStatusMessageText(msg *a2a.Message) string {
	for _, part := range msg.Parts {
		if part == nil {
			continue
		}
		if _, ok := part.Content.(a2a.Text); ok {
			return part.Text()
		}
	}
	return ""
}
