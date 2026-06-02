package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultBroadcastTimeout = 30
	minBroadcastTimeout     = 1
	maxBroadcastTimeout     = 120
)

// broadcastResult holds the outcome of sending a message to a single agent.
type broadcastResult struct {
	Status   string `json:"status"`
	Response string `json:"response,omitempty"`
	Error    string `json:"error,omitempty"`
}

// handleBroadcastMessage sends the same message to multiple agents simultaneously
// and collects responses.
func (s *Server) handleBroadcastMessage(ctx context.Context, _ *mcp.CallToolRequest, input BroadcastMessageInput) (*mcp.CallToolResult, any, error) {
	// Validate aliases.
	if err := ValidateBroadcastAliases(input.Aliases); err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}

	// Validate message.
	if err := ValidateMessage(input.Message); err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}

	// Determine per-agent timeout.
	timeoutSeconds := defaultBroadcastTimeout
	if input.TimeoutSeconds != nil {
		timeoutSeconds = *input.TimeoutSeconds
		if timeoutSeconds < minBroadcastTimeout || timeoutSeconds > maxBroadcastTimeout {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("timeout_seconds must be between %d and %d, got %d", minBroadcastTimeout, maxBroadcastTimeout, timeoutSeconds)}},
			}, nil, nil
		}
	}

	// Launch one goroutine per alias for concurrent execution.
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		results = make(map[string]*broadcastResult)
	)

	for _, alias := range input.Aliases {
		wg.Add(1)
		go func(alias string) {
			defer wg.Done()
			result := s.broadcastToAgent(ctx, alias, input.Message, timeoutSeconds)
			mu.Lock()
			results[alias] = result
			mu.Unlock()
		}(alias)
	}

	wg.Wait()

	// Serialize results as JSON.
	resultJSON, err := json.Marshal(results)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to serialize broadcast results: %v", err)}},
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(resultJSON)}},
	}, nil, nil
}

// broadcastToAgent sends a message to a single agent and returns the result.
func (s *Server) broadcastToAgent(ctx context.Context, alias, message string, timeoutSeconds int) *broadcastResult {
	// Resolve alias from registry.
	entry := s.registry.Lookup(alias)
	if entry == nil {
		return &broadcastResult{
			Status: "error",
			Error:  fmt.Sprintf("alias %q is not registered", alias),
		}
	}

	// Create per-agent timeout context.
	agentCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	// Build A2A message request.
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(message))
	sendReq := &a2a.SendMessageRequest{
		Message: msg,
	}

	// Serialize the request body.
	reqBody, err := json.Marshal(sendReq)
	if err != nil {
		return &broadcastResult{
			Status: "error",
			Error:  fmt.Sprintf("failed to serialize request: %v", err),
		}
	}

	// Get HTTP client with agent headers.
	client := httpClientForAgent(s.httpClient, entry)

	// Send HTTP POST to agent URL.
	httpReq, err := http.NewRequestWithContext(agentCtx, http.MethodPost, entry.URL, bytes.NewReader(reqBody))
	if err != nil {
		return &broadcastResult{
			Status: "error",
			Error:  fmt.Sprintf("failed to create request: %v", err),
		}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		return &broadcastResult{
			Status: "error",
			Error:  fmt.Sprintf("agent unreachable: %v", err),
		}
	}
	defer resp.Body.Close()

	// Read response body.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &broadcastResult{
			Status: "error",
			Error:  fmt.Sprintf("failed to read response: %v", err),
		}
	}

	// Parse the response as a Task.
	var task a2a.Task
	if err := json.Unmarshal(body, &task); err != nil {
		return &broadcastResult{
			Status: "error",
			Error:  fmt.Sprintf("failed to parse response: %v", err),
		}
	}

	// Handle task states.
	switch task.Status.State {
	case a2a.TaskStateCompleted:
		text, _ := extractTextFromArtifacts(task.Artifacts)
		return &broadcastResult{
			Status:   "success",
			Response: text,
		}
	case a2a.TaskStateInputRequired:
		// input-required is terminal for the current turn; surface the agent's message.
		responseText := ""
		if task.Status.Message != nil {
			responseText = extractStatusMessageText(task.Status.Message)
		}
		if responseText == "" {
			text, _ := extractTextFromArtifacts(task.Artifacts)
			responseText = text
		}
		return &broadcastResult{
			Status:   "input-required",
			Response: responseText,
		}
	case a2a.TaskStateFailed:
		failMsg := "task failed"
		if task.Status.Message != nil {
			text := extractStatusMessageText(task.Status.Message)
			if text != "" {
				failMsg = text
			}
		}
		return &broadcastResult{
			Status: "error",
			Error:  failMsg,
		}
	case a2a.TaskStateCanceled:
		return &broadcastResult{
			Status: "error",
			Error:  "task was canceled by the agent",
		}
	default:
		return &broadcastResult{
			Status: "error",
			Error:  fmt.Sprintf("timeout waiting for task completion (state: %s)", task.Status.State),
		}
	}
}
