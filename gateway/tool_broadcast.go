package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	Status   string       `json:"status"`
	Response string       `json:"response,omitempty"`
	Error    string       `json:"error,omitempty"`
	Task     *a2a.Task    `json:"task,omitempty"`
	Message  *a2a.Message `json:"message,omitempty"`
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
	type agentOutcome struct {
		result   *broadcastResult
		response *SendMessageResponse
	}

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		outcomes = make(map[string]*agentOutcome)
	)

	for _, alias := range input.Aliases {
		wg.Add(1)
		go func(alias string) {
			defer wg.Done()
			result, resp := s.broadcastToAgent(ctx, alias, input.Message, input.Parts, input.Metadata, timeoutSeconds)
			mu.Lock()
			outcomes[alias] = &agentOutcome{result: result, response: resp}
			mu.Unlock()
		}(alias)
	}

	wg.Wait()

	// Build legacy text results and structured content map.
	results := make(map[string]*broadcastResult, len(outcomes))
	structured := make(map[string]*SendMessageResponse, len(outcomes))
	for alias, outcome := range outcomes {
		results[alias] = outcome.result
		if outcome.response != nil {
			structured[alias] = outcome.response
		}
	}

	// Serialize results as JSON (legacy text content).
	resultJSON, err := json.Marshal(results)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to serialize broadcast results: %v", err)}},
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(resultJSON)}},
	}, structured, nil
}

// broadcastSentText returns the text representation of the sent message for history recording
// in the broadcast context. It mirrors sentMessageText but works with broadcast parameters.
func broadcastSentText(message string, parts []InputPart) string {
	if message != "" {
		return message
	}
	var segments []string
	for _, p := range parts {
		switch {
		case p.Text != nil:
			segments = append(segments, *p.Text)
		case p.Data != nil:
			segments = append(segments, "[data]")
		case p.URL != nil:
			segments = append(segments, *p.URL)
		case p.Raw != nil:
			segments = append(segments, "[binary]")
		}
	}
	return strings.Join(segments, "")
}

// broadcastToAgent sends a message to a single agent and returns the result.
// The second return value is a *SendMessageResponse for structured content (nil on error/skip).
func (s *Server) broadcastToAgent(ctx context.Context, alias, message string, parts []InputPart, metadata map[string]any, timeoutSeconds int) (*broadcastResult, *SendMessageResponse) {
	// Resolve alias from registry.
	entry := s.registry.Lookup(alias)
	if entry == nil {
		result := &broadcastResult{
			Status: "error",
			Error:  fmt.Sprintf("alias %q is not registered", alias),
		}
		s.recordBroadcastHistory(ctx, alias, message, parts, result)
		return result, nil
	}

	// Health check BEFORE rate limit — skip unhealthy agents without consuming
	// a rate limit token (HLTH-6.1, HLTH-6.4). Agents with status "unknown" are
	// still attempted (HLTH-6.3).
	if s.healthTracker.IsEnabled() {
		state := s.healthTracker.Get(alias)
		if state.Status == HealthStatusUnhealthy {
			result := &broadcastResult{Status: "skipped", Error: "agent is unhealthy"}
			s.recordBroadcastHistory(ctx, alias, message, parts, result)
			return result, nil
		}
	}

	// Rate limit check for this agent.
	if !s.rateLimiters.Allow(alias) {
		result := &broadcastResult{
			Status: "error",
			Error:  fmt.Sprintf("rate limited: agent %q has exceeded its rate limit", alias),
		}
		s.recordBroadcastHistory(ctx, alias, message, parts, result)
		return result, nil
	}

	// Create per-agent timeout context.
	agentCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	// Resolve SDK client for this agent.
	resolved := &ResolveResult{URL: entry.URL, IsAlias: true, Headers: entry.Headers, Alias: alias}
	a2aClient, err := s.clients.Resolve(agentCtx, resolved)
	if err != nil {
		result := &broadcastResult{
			Status: "error",
			Error:  fmt.Sprintf("failed to resolve client: %v", err),
		}
		s.recordBroadcastHistory(ctx, alias, message, parts, result)
		return result, nil
	}

	// Build content parts from message/parts input.
	contentParts, err := buildMessageParts(message, parts)
	if err != nil {
		result := &broadcastResult{
			Status: "error",
			Error:  err.Error(),
		}
		s.recordBroadcastHistory(ctx, alias, message, parts, result)
		return result, nil
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
		result, resp, reachable := s.broadcastToAgentStreaming(agentCtx, a2aClient, sendReq, resolved, alias, timeoutSeconds)
		// Record health outcome for streaming path.
		if reachable {
			s.healthTracker.RecordSuccess(alias)
		} else {
			// Transport-level failure — classify and record.
			s.recordBroadcastHealthOutcome(agentCtx, alias, fmt.Errorf("%s", result.Error))
		}
		s.recordBroadcastHistory(ctx, alias, message, parts, result)
		return result, resp
	}

	sendResult, err := a2aClient.SendMessage(agentCtx, sendReq)
	if err != nil {
		// Record health outcome for connection errors.
		s.recordBroadcastHealthOutcome(agentCtx, alias, err)
		result := &broadcastResult{
			Status: "error",
			Error:  err.Error(),
		}
		s.recordBroadcastHistory(ctx, alias, message, parts, result)
		return result, nil
	}

	// Successful HTTP response — record success.
	s.healthTracker.RecordSuccess(alias)

	// Type switch on result.
	switch v := sendResult.(type) {
	case *a2a.Message:
		// Requirement: SRES-4.2 — broadcast includes raw message
		text := extractContentFromMessageParts(v.Parts)
		result := &broadcastResult{
			Status:   "success",
			Response: text,
			Message:  v,
		}
		s.recordBroadcastHistory(ctx, alias, message, parts, result)
		return result, &SendMessageResponse{Message: v}

	case *a2a.Task:
		result, resp := s.handleBroadcastTaskResult(v)
		s.recordBroadcastHistory(ctx, alias, message, parts, result)
		return result, resp

	default:
		result := &broadcastResult{
			Status: "error",
			Error:  "unrecognized response format",
		}
		s.recordBroadcastHistory(ctx, alias, message, parts, result)
		return result, nil
	}
}

// recordBroadcastHistory records a broadcast interaction to the history backend.
func (s *Server) recordBroadcastHistory(ctx context.Context, alias, message string, parts []InputPart, result *broadcastResult) {
	sent := broadcastSentText(message, parts)
	response := result.Response
	if response == "" && result.Error != "" {
		response = result.Error
	}
	// HLTH-6.6: skipped agents are recorded with the same semantics as an error result.
	isError := result.Status == "error" || result.Status == "skipped"
	s.recordHistory(ctx, alias, sent, response, "", "", isError)
}

// recordBroadcastHealthOutcome classifies an error and records the appropriate
// health outcome. Connection errors trigger RecordFailure; context cancellations
// are ignored (HLTH-8.3).
func (s *Server) recordBroadcastHealthOutcome(_ context.Context, alias string, err error) {
	outcome := ClassifyError(err)
	switch outcome {
	case OutcomeConnectionError:
		s.healthTracker.RecordFailure(alias)
	case OutcomeSuccess:
		s.healthTracker.RecordSuccess(alias)
	case OutcomeContextCanceled:
		// Do not update health state for client-initiated cancellations.
	}
}

// broadcastToAgentStreaming sends a message using streaming and converts
// the result to a broadcastResult with the same shape as non-streaming.
// Requirements: STRM-5.1, STRM-5.3, STRM-5.4
// Returns:
//   - *broadcastResult: legacy text result
//   - *SendMessageResponse: structured content (nil on transport error)
//   - bool: whether the agent was reachable (true = response received, false = transport error)
func (s *Server) broadcastToAgentStreaming(
	ctx context.Context,
	a2aClient *a2aclient.Client,
	sendReq *a2a.SendMessageRequest,
	resolved *ResolveResult,
	alias string,
	timeoutSeconds int,
) (*broadcastResult, *SendMessageResponse, bool) {
	// Requirement: STRM-5.3 — per-agent broadcast timeout is already applied
	// via the parent ctx (agentCtx) from broadcastToAgent. No additional
	// timeout needed here.
	events := a2aClient.SendStreamingMessage(ctx, sendReq)

	state := &streamState{}
	result, err := consumeStream(ctx, events, state)
	if err != nil {
		return &broadcastResult{Status: "error", Error: err.Error()}, nil, false
	}

	// Convert streamResult to broadcastResult using the same logic as non-streaming path.
	switch {
	case result.task != nil:
		br, resp := s.handleBroadcastTaskResult(result.task)
		return br, resp, true

	case result.message != nil:
		text := extractContentFromMessageParts(result.message.Parts)
		return &broadcastResult{Status: "success", Response: text, Message: result.message}, &SendMessageResponse{Message: result.message}, true

	case result.terminatedByStatus:
		task := buildTaskFromState(result.state)
		br, resp := s.handleBroadcastTaskResult(task)
		return br, resp, true

	default:
		return &broadcastResult{Status: "error", Error: "no terminal event received"}, nil, false
	}
}

// handleBroadcastTaskResult processes a task result in the broadcast context.
// Returns the legacy broadcastResult and a *SendMessageResponse for structured content.
func (s *Server) handleBroadcastTaskResult(task *a2a.Task) (*broadcastResult, *SendMessageResponse) {
	// Requirement: SRES-4.1, SRES-4.3 — broadcast includes raw task in successful results
	switch task.Status.State {
	case a2a.TaskStateCompleted:
		text := extractContentFromArtifacts(task.Artifacts)
		return &broadcastResult{
			Status:   "success",
			Response: text,
			Task:     task,
		}, &SendMessageResponse{Task: task}
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
			Task:     task,
		}, &SendMessageResponse{Task: task}
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
			Task:     task,
		}, &SendMessageResponse{Task: task}
	case a2a.TaskStateFailed:
		// Requirement: SRES-4.4 — error/skipped results omit task and message
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
		}, nil
	case a2a.TaskStateCanceled:
		// Requirement: SRES-4.4 — error/skipped results omit task and message
		return &broadcastResult{
			Status: "error",
			Error:  "task was canceled by the agent",
		}, nil
	default:
		return &broadcastResult{
			Status: "error",
			Error:  fmt.Sprintf("timeout waiting for task completion (state: %s)", task.Status.State),
		}, nil
	}
}
