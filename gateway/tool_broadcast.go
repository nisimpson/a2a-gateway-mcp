package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
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

	// Validate that at least one of message or parts is provided.
	if input.Message == "" && len(input.Parts) == 0 {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "either 'message' or 'parts' is required"}},
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
			result := s.broadcastToAgent(ctx, alias, input.Message, input.Parts, input.Metadata, timeoutSeconds)
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
func (s *Server) broadcastToAgent(ctx context.Context, alias, message string, parts []InputPart, metadata map[string]any, timeoutSeconds int) *broadcastResult {
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

	// Resolve SDK client for this agent.
	resolved := &ResolveResult{URL: entry.URL, IsAlias: true, Headers: entry.Headers, Alias: alias}
	a2aClient, err := s.clients.Resolve(agentCtx, resolved)
	if err != nil {
		return &broadcastResult{
			Status: "error",
			Error:  fmt.Sprintf("failed to resolve client: %v", err),
		}
	}

	// Build content parts from message/parts input.
	contentParts, err := buildMessageParts(message, parts)
	if err != nil {
		return &broadcastResult{
			Status: "error",
			Error:  err.Error(),
		}
	}

	// Build A2A message.
	msg := a2a.NewMessage(a2a.MessageRoleUser, contentParts...)

	// Build SendMessageRequest.
	sendReq := &a2a.SendMessageRequest{
		Message: msg,
	}
	if len(metadata) > 0 {
		sendReq.Metadata = metadata
	}
	sendReq.Metadata = s.injectCallerCard(sendReq.Metadata)

	// Requirement: STRM-5.1, STRM-5.2 — use streaming when supported.
	if supportsStreaming(s.registry, resolved) {
		return s.broadcastToAgentStreaming(agentCtx, a2aClient, sendReq, resolved, alias, timeoutSeconds)
	}

	result, err := a2aClient.SendMessage(agentCtx, sendReq)
	if err != nil {
		return &broadcastResult{
			Status: "error",
			Error:  err.Error(),
		}
	}

	// Type switch on result.
	switch v := result.(type) {
	case *a2a.Message:
		text := extractContentFromMessageParts(v.Parts)
		return &broadcastResult{
			Status:   "success",
			Response: text,
		}

	case *a2a.Task:
		return s.handleBroadcastTaskResult(v)

	default:
		return &broadcastResult{
			Status: "error",
			Error:  "unrecognized response format",
		}
	}
}

// broadcastToAgentStreaming sends a message using streaming and converts
// the result to a broadcastResult with the same shape as non-streaming.
// Requirements: STRM-5.1, STRM-5.3, STRM-5.4
func (s *Server) broadcastToAgentStreaming(
	ctx context.Context,
	a2aClient *a2aclient.Client,
	sendReq *a2a.SendMessageRequest,
	resolved *ResolveResult,
	alias string,
	timeoutSeconds int,
) *broadcastResult {
	// Requirement: STRM-5.3 — per-agent broadcast timeout is already applied
	// via the parent ctx (agentCtx) from broadcastToAgent. No additional
	// timeout needed here.
	events := a2aClient.SendStreamingMessage(ctx, sendReq)

	state := &streamState{}
	result, err := consumeStream(ctx, events, state)
	if err != nil {
		return &broadcastResult{Status: "error", Error: err.Error()}
	}

	// Convert streamResult to broadcastResult using the same logic as non-streaming path.
	switch {
	case result.task != nil:
		return s.handleBroadcastTaskResult(result.task)

	case result.message != nil:
		text := extractContentFromMessageParts(result.message.Parts)
		return &broadcastResult{Status: "success", Response: text}

	case result.terminatedByStatus:
		task := buildTaskFromState(result.state)
		return s.handleBroadcastTaskResult(task)

	default:
		return &broadcastResult{Status: "error", Error: "no terminal event received"}
	}
}

// handleBroadcastTaskResult processes a task result in the broadcast context.
func (s *Server) handleBroadcastTaskResult(task *a2a.Task) *broadcastResult {
	switch task.Status.State {
	case a2a.TaskStateCompleted:
		text := extractContentFromArtifacts(task.Artifacts)
		return &broadcastResult{
			Status:   "success",
			Response: text,
		}
	case a2a.TaskStateInputRequired:
		responseText := ""
		if task.Status.Message != nil {
			responseText = extractStatusMessageText(task.Status.Message)
		}
		if responseText == "" {
			responseText = extractContentFromArtifacts(task.Artifacts)
		}
		return &broadcastResult{
			Status:   "input-required",
			Response: responseText,
		}
	case a2a.TaskStateAuthRequired:
		responseText := ""
		if task.Status.Message != nil {
			responseText = extractStatusMessageText(task.Status.Message)
		}
		if responseText == "" {
			responseText = extractContentFromArtifacts(task.Artifacts)
		}
		return &broadcastResult{
			Status:   "auth-required",
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
