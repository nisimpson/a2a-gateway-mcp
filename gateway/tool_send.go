package gateway

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// extractResultText extracts the primary response text from a CallToolResult.
// It returns the text from the first TextContent item, skipping metadata
// prefixes (context_id:, task_id:, state:).
func extractResultText(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}
	var parts []string
	for _, content := range result.Content {
		tc, ok := content.(*mcp.TextContent)
		if !ok {
			continue
		}
		// Skip metadata content items.
		if strings.HasPrefix(tc.Text, "context_id:") ||
			strings.HasPrefix(tc.Text, "task_id:") ||
			strings.HasPrefix(tc.Text, "state:") {
			continue
		}
		parts = append(parts, tc.Text)
	}
	return strings.Join(parts, "\n")
}

// extractResultContextID extracts the context_id from a CallToolResult's content items.
func extractResultContextID(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}
	for _, content := range result.Content {
		tc, ok := content.(*mcp.TextContent)
		if !ok {
			continue
		}
		if v, found := strings.CutPrefix(tc.Text, "context_id:"); found {
			return v
		}
	}
	return ""
}

// extractResultTaskID extracts the task_id from a CallToolResult's content items.
func extractResultTaskID(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}
	for _, content := range result.Content {
		tc, ok := content.(*mcp.TextContent)
		if !ok {
			continue
		}
		if v, found := strings.CutPrefix(tc.Text, "task_id:"); found {
			return v
		}
	}
	return ""
}

// sentMessageText returns the text representation of the sent message for history recording.
// It uses the plain message field if available, otherwise renders the parts.
func sentMessageText(input SendMessageInput) string {
	if input.Message != "" {
		return input.Message
	}
	// Build a summary from parts.
	var parts []string
	for _, p := range input.Parts {
		switch {
		case p.Text != nil:
			parts = append(parts, *p.Text)
		case p.Data != nil:
			parts = append(parts, "[data]")
		case p.URL != nil:
			parts = append(parts, *p.URL)
		case p.Raw != nil:
			parts = append(parts, "[binary]")
		}
	}
	return strings.Join(parts, "")
}

// buildMessageParts constructs a2a.ContentParts from the input message and parts.
// Priority: parts > message. At least one must be provided.
func buildMessageParts(message string, parts []InputPart) (a2a.ContentParts, error) {
	if len(parts) > 0 {
		return convertInputParts(parts)
	}
	if message != "" {
		return a2a.ContentParts{a2a.NewTextPart(message)}, nil
	}
	return nil, fmt.Errorf("either 'message' or 'parts' is required")
}

// convertInputParts converts a slice of InputPart to a2a.ContentParts.
// Each InputPart must have exactly one of Text, Data, URL, or Raw set.
func convertInputParts(parts []InputPart) (a2a.ContentParts, error) {
	var result a2a.ContentParts
	for i, p := range parts {
		count := 0
		if p.Text != nil {
			count++
		}
		if p.Data != nil {
			count++
		}
		if p.URL != nil {
			count++
		}
		if p.Raw != nil {
			count++
		}
		if count == 0 {
			return nil, fmt.Errorf("part at index %d has no content (set exactly one of text, data, url, or raw)", i)
		}
		if count > 1 {
			return nil, fmt.Errorf("part at index %d has multiple content types (set exactly one of text, data, url, or raw)", i)
		}
		switch {
		case p.Text != nil:
			result = append(result, a2a.NewTextPart(*p.Text))
		case p.Data != nil:
			result = append(result, &a2a.Part{Content: a2a.Data{Value: p.Data}})
		case p.URL != nil:
			result = append(result, &a2a.Part{Content: a2a.URL(*p.URL)})
		case p.Raw != nil:
			decoded, err := base64.StdEncoding.DecodeString(*p.Raw)
			if err != nil {
				return nil, fmt.Errorf("part at index %d has invalid base64 in 'raw' field: %v", i, err)
			}
			result = append(result, &a2a.Part{Content: a2a.Raw(decoded)})
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("'parts' must contain at least one element")
	}
	return result, nil
}

const (
	taskPollInterval = 2 * time.Second
	taskPollTimeout  = 60 * time.Second
)

// effectivePollTimeout returns the poll timeout to use for a request.
// Per-request PollTimeoutSeconds takes precedence over the server default.
// A negative value means no timeout (wait indefinitely).
func (s *Server) effectivePollTimeout(requestSeconds *int) time.Duration {
	if requestSeconds != nil {
		if *requestSeconds < 0 {
			return 0 // sentinel: no timeout
		}
		if *requestSeconds > 0 {
			return time.Duration(*requestSeconds) * time.Second
		}
	}
	return s.pollTimeout
}

// handleSendMessage sends a message to a connected A2A agent by alias or URL.
func (s *Server) handleSendMessage(ctx context.Context, req *mcp.CallToolRequest, input SendMessageInput) (*mcp.CallToolResult, any, error) {
	// Validate agent identifier is provided.
	if input.Agent == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "agent identifier is required"}},
		}, nil, nil
	}

	// Build content parts from message/parts input.
	contentParts, err := buildMessageParts(input.Message, input.Parts)
	if err != nil {
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

	// Rate limit check for alias-based sends.
	if resolved.IsAlias {
		if result := s.checkRateLimit(resolved.Alias); result != nil {
			return result, nil, nil
		}
	}

	// Determine context_id priority: explicit > stored > empty (new conversation).
	contextID := input.ContextID
	if contextID == "" && resolved.IsAlias {
		contextID = s.contextStore.Get(input.Agent)
	}

	// Build A2A message with content parts.
	msg := a2a.NewMessage(a2a.MessageRoleUser, contentParts...)
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
	if len(input.Metadata) > 0 {
		sendReq.Metadata = input.Metadata
	}
	// Requirement: CAC-2.1 — inject caller card into outbound metadata
	sendReq.Metadata = s.injectCallerCard(sendReq.Metadata)

	// Resolve SDK client.
	a2aClient, err := s.clients.Resolve(ctx, resolved)
	if err != nil {
		return handleA2AError(err), nil, nil
	}

	// Requirement: STRM-1.1, STRM-1.5 — route to streaming when supported.
	if supportsStreaming(s.registry, resolved) {
		result, err := s.handleStreamingMessage(ctx, req, a2aClient, sendReq, resolved, input.Agent, s.effectiveStreamTimeout(input.PollTimeoutSeconds))
		if err != nil {
			// Requirement: HLTH-1.1, HLTH-8.3 — record health based on error classification.
			if resolved.IsAlias {
				outcome := ClassifyError(err)
				if outcome == OutcomeConnectionError {
					s.healthTracker.RecordFailure(resolved.Alias)
				}
				// On OutcomeContextCanceled: do not update health state (HLTH-8.3).
			}
			errResult := handleA2AError(err)
			s.recordHistory(ctx, input.Agent, sentMessageText(input), extractResultText(errResult), extractResultContextID(errResult), extractResultTaskID(errResult), errResult.IsError)
			return errResult, nil, nil
		}
		// Requirement: HLTH-1.2 — record success on any HTTP response.
		if resolved.IsAlias {
			s.healthTracker.RecordSuccess(resolved.Alias)
		}
		s.recordHistory(ctx, input.Agent, sentMessageText(input), extractResultText(result), extractResultContextID(result), extractResultTaskID(result), result.IsError)
		return result, nil, nil
	}

	// Call SDK client.
	result, err := a2aClient.SendMessage(ctx, sendReq)
	if err != nil {
		// Requirement: HLTH-1.1, HLTH-8.3 — record health based on error classification.
		if resolved.IsAlias {
			outcome := ClassifyError(err)
			if outcome == OutcomeConnectionError {
				s.healthTracker.RecordFailure(resolved.Alias)
			}
			// On OutcomeContextCanceled: do not update health state (HLTH-8.3).
		}
		errResult := handleA2AError(err)
		s.recordHistory(ctx, input.Agent, sentMessageText(input), extractResultText(errResult), extractResultContextID(errResult), extractResultTaskID(errResult), errResult.IsError)
		return errResult, nil, nil
	}

	// Requirement: HLTH-1.2 — record success on any HTTP response.
	if resolved.IsAlias {
		s.healthTracker.RecordSuccess(resolved.Alias)
	}

	// Type switch on result.
	switch v := result.(type) {
	case *a2a.Task:
		taskResult, meta, taskErr := s.handleTaskResult(ctx, a2aClient, v, resolved, input.Agent, s.effectivePollTimeout(input.PollTimeoutSeconds))
		s.recordHistory(ctx, input.Agent, sentMessageText(input), extractResultText(taskResult), extractResultContextID(taskResult), extractResultTaskID(taskResult), taskResult.IsError)
		return taskResult, meta, taskErr
	case *a2a.Message:
		// Store context_id if alias-based.
		if resolved.IsAlias && v.ContextID != "" {
			s.contextStore.Set(input.Agent, v.ContextID)
		}
		msgResult := FormatMessageResponse(v)
		s.recordHistory(ctx, input.Agent, sentMessageText(input), extractResultText(msgResult), extractResultContextID(msgResult), extractResultTaskID(msgResult), msgResult.IsError)
		return msgResult, nil, nil
	default:
		defaultResult := &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "unrecognized response format: expected Task or Message"}},
		}
		s.recordHistory(ctx, input.Agent, sentMessageText(input), extractResultText(defaultResult), extractResultContextID(defaultResult), extractResultTaskID(defaultResult), defaultResult.IsError)
		return defaultResult, nil, nil
	}
}

// handleTaskResult processes a *a2a.Task result from SendMessage, handling
// all task states including polling for non-terminal states.
func (s *Server) handleTaskResult(ctx context.Context, a2aClient *a2aclient.Client, task *a2a.Task, resolved *ResolveResult, agent string, pollTimeout time.Duration) (*mcp.CallToolResult, any, error) {
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

	case a2a.TaskStateWorking, a2a.TaskStateSubmitted:
		// Known non-terminal states — poll for completion if task has an ID.
		if task.ID == "" {
			if resolved.IsAlias && task.ContextID != "" {
				s.contextStore.Set(agent, task.ContextID)
			}
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("agent returned non-terminal state %q without a task ID; cannot poll for completion", task.Status.State)}},
			}, nil, nil
		}

		// Poll for non-terminal states up to the configured timeout.
		polledTask, err := s.pollTaskCompletion(ctx, a2aClient, task.ID, pollTimeout)
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

	default:
		// Unrecognized task state — likely a v0.x agent or protocol mismatch.
		if resolved.IsAlias && task.ContextID != "" {
			s.contextStore.Set(agent, task.ContextID)
		}
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("agent returned unrecognized task state %q — ensure the agent supports A2A protocol v1.0 or later", task.Status.State)}},
		}, nil, nil
	}
}

// pollTaskCompletion polls the agent for task status every 2s until a terminal
// state is reached or the given timeout elapses. A zero timeout means no deadline.
func (s *Server) pollTaskCompletion(ctx context.Context, a2aClient *a2aclient.Client, taskID a2a.TaskID, timeout time.Duration) (*a2a.Task, error) {
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	ticker := time.NewTicker(taskPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for task completion: context canceled")
		case <-ticker.C:
			if !deadline.IsZero() && time.Now().After(deadline) {
				return nil, fmt.Errorf("timeout waiting for task completion after %s", timeout)
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

// extractStatusMessageText renders all parts from a task status message using
// renderPart. Parts are concatenated in order with no separator.
func extractStatusMessageText(msg *a2a.Message) string {
	var parts []string
	for _, part := range msg.Parts {
		if rendered, ok := renderPart(part); ok {
			parts = append(parts, rendered)
		}
	}
	return strings.Join(parts, "")
}
