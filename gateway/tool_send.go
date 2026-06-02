package gateway

import (
	"context"
	"fmt"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	taskPollInterval = 2 * time.Second
	taskPollTimeout  = 60 * time.Second
)

// handleSendMessage sends a text message to a connected A2A agent by alias or URL.
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
	if input.TaskID != "" {
		msg.TaskID = a2a.TaskID(input.TaskID)
	}

	// Build SendMessageRequest.
	sendReq := &a2a.SendMessageRequest{
		Message: msg,
	}

	// Resolve SDK client.
	a2aClient, err := s.clients.Resolve(ctx, resolved)
	if err != nil {
		return handleA2AError(err), nil, nil
	}

	// Call SDK client.
	result, err := a2aClient.SendMessage(ctx, sendReq)
	if err != nil {
		return handleA2AError(err), nil, nil
	}

	// Type switch on result.
	switch v := result.(type) {
	case *a2a.Task:
		return s.handleTaskResult(ctx, a2aClient, v, resolved, input.Agent)
	case *a2a.Message:
		// Store context_id if alias-based.
		if resolved.IsAlias && v.ContextID != "" {
			s.contextStore.Set(input.Agent, v.ContextID)
		}
		return FormatMessageResponse(v), nil, nil
	default:
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "unrecognized response format: expected Task or Message"}},
		}, nil, nil
	}
}

// handleTaskResult processes a *a2a.Task result from SendMessage, handling
// all task states including polling for non-terminal states.
func (s *Server) handleTaskResult(ctx context.Context, a2aClient *a2aclient.Client, task *a2a.Task, resolved *ResolveResult, agent string) (*mcp.CallToolResult, any, error) {
	switch task.Status.State {
	case a2a.TaskStateCompleted:
		if resolved.IsAlias && task.ContextID != "" {
			s.contextStore.Set(agent, task.ContextID)
		}
		return FormatTaskResponse(task), nil, nil

	case a2a.TaskStateInputRequired:
		if resolved.IsAlias && task.ContextID != "" {
			s.contextStore.Set(agent, task.ContextID)
		}
		return FormatInputRequiredResponse(task), nil, nil

	case a2a.TaskStateAuthRequired:
		if resolved.IsAlias && task.ContextID != "" {
			s.contextStore.Set(agent, task.ContextID)
		}
		return FormatInterruptedResponse(task, "auth-required"), nil, nil

	case a2a.TaskStateFailed:
		if resolved.IsAlias && task.ContextID != "" {
			s.contextStore.Set(agent, task.ContextID)
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
		if resolved.IsAlias && task.ContextID != "" {
			s.contextStore.Set(agent, task.ContextID)
		}
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "task was canceled by the agent"}},
		}, nil, nil

	default:
		// Guard: if the task has no ID, the agent likely doesn't support tasks/get.
		if task.ID == "" {
			if resolved.IsAlias && task.ContextID != "" {
				s.contextStore.Set(agent, task.ContextID)
			}
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("agent returned non-terminal state %q without a task ID; cannot poll for completion", task.Status.State)}},
			}, nil, nil
		}

		// Poll up to 60s for non-terminal states (e.g. working, submitted).
		polledTask, err := s.pollTaskCompletion(ctx, a2aClient, task.ID)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, nil, nil
		}

		// Re-evaluate the terminal state after polling.
		if resolved.IsAlias && polledTask.ContextID != "" {
			s.contextStore.Set(agent, polledTask.ContextID)
		}
		switch polledTask.Status.State {
		case a2a.TaskStateCompleted:
			return FormatTaskResponse(polledTask), nil, nil
		case a2a.TaskStateInputRequired:
			return FormatInputRequiredResponse(polledTask), nil, nil
		case a2a.TaskStateAuthRequired:
			return FormatInterruptedResponse(polledTask, "auth-required"), nil, nil
		case a2a.TaskStateFailed:
			failMsg := "task failed"
			if polledTask.Status.Message != nil {
				text := extractStatusMessageText(polledTask.Status.Message)
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
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("timeout waiting for task completion (state: %s)", polledTask.Status.State)}},
			}, nil, nil
		}
	}
}

// pollTaskCompletion polls the agent for task status every 2s until a terminal
// state is reached or 60s elapses.
func (s *Server) pollTaskCompletion(ctx context.Context, a2aClient *a2aclient.Client, taskID a2a.TaskID) (*a2a.Task, error) {
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

			task, err := a2aClient.GetTask(ctx, &a2a.GetTaskRequest{ID: taskID})
			if err != nil {
				return nil, fmt.Errorf("failed to poll task status: %v", err)
			}

			switch task.Status.State {
			case a2a.TaskStateCompleted, a2a.TaskStateFailed, a2a.TaskStateCanceled, a2a.TaskStateInputRequired, a2a.TaskStateAuthRequired:
				return task, nil
			}
			// Still non-terminal, continue polling.
		}
	}
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
