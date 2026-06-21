package tool

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nisimpson/a2a-gateway-mcp/internal/health"
	"github.com/nisimpson/a2a-gateway-mcp/internal/history"
)

// InputPart represents a single content part in a multi-part message.
// Exactly one of Text, Data, URL, or Raw should be set.
type InputPart struct {
	// Text contains plain text content.
	Text *string `json:"text,omitempty" jsonschema:"plain text content"`
	// Data contains structured JSON data to send as a DataPart.
	Data any `json:"data,omitempty" jsonschema:"structured JSON data (object, array, or primitive)"`
	// URL contains a URL reference to send as a URLPart.
	URL *string `json:"url,omitempty" jsonschema:"URL reference"`
	// Raw contains base64-encoded binary data to send as a RawPart.
	Raw *string `json:"raw,omitempty" jsonschema:"base64-encoded binary data (standard base64, RFC 4648). Decode with base64.StdEncoding."`
}

// SendMessageInput is the input schema for the send_message tool.
type SendMessageInput struct {
	Agent              string         `json:"agent" jsonschema:"agent alias from registry or full HTTP/HTTPS URL"`
	Message            string         `json:"message,omitempty" jsonschema:"plain text message to send. Use this for simple text-only messages. Mutually exclusive with 'parts' — if both are provided, 'parts' takes precedence."`
	Parts              []InputPart    `json:"parts,omitempty" jsonschema:"structured message parts for multi-part or non-text content. Use this when sending JSON data, URLs, or mixed content. Takes precedence over 'message' if both are provided."`
	ContextID          string         `json:"context_id,omitempty" jsonschema:"optional context ID to continue an existing conversation"`
	TaskID             string         `json:"task_id,omitempty" jsonschema:"optional task ID to reference an existing task for follow-up messages"`
	Metadata           map[string]any `json:"metadata,omitempty" jsonschema:"optional metadata for A2A protocol extensions (e.g. caller capabilities)"`
	PollTimeoutSeconds *int           `json:"poll_timeout_seconds,omitempty" jsonschema:"max seconds to wait for task completion when polling or streaming (negative = no timeout, default: server configured timeout)"`
}

// output_schemas.go defines output schema types and JSON schema literals
// for all gateway tools. These are used to advertise structuredContent shapes
// via the MCP protocol's OutputSchema field.
//
// Two strategies are used:
// 1. Embedded JSON schemas from internal/specification for send_message and
//    broadcast_message (derived from the A2A specification).
// 2. Output structs with jsonschema: tags for all other tools, letting the
//    MCP SDK reflect on them to auto-populate OutputSchema.
//
// Requirements: SRES-1.2, SRES-6.1

// SendMessageOutput wraps the A2A response in a typed envelope.
// Exactly one of Message or Task will be non-nil.
type SendMessageOutput struct {
	Message *a2a.Message `json:"message,omitempty"`
	Task    *a2a.Task    `json:"task,omitempty"`
}

// SendMessageTool sends a message to a connected A2A agent.
type SendMessageTool struct {
	AgentRegistry          AgentRegistry
	A2AClientResolver      A2AClientResolver
	ContextStore           ContextStore
	CallerCardInjector     CallerCardInjector
	HealthTracker          HealthTracker
	HistoryRecorder        HistoryRecorder
	RateLimiter            RateLimiter
	EffectivePollTimeout   EffectiveTimeoutFunc
	EffectiveStreamTimeout EffectiveTimeoutFunc
}

// NewSendMessageTool constructs a SendMessageTool with all dependencies
// wired from the shared Env configuration. The returned tool is ready to be
// registered with the MCP server for handling send_message requests.
func NewSendMessageTool(env *Env) *SendMessageTool {
	return &SendMessageTool{
		AgentRegistry:          env.AgentRegistry,
		A2AClientResolver:      env.A2AClientResolver,
		ContextStore:           env.ContextStore,
		CallerCardInjector:     env.CallerCardInjector,
		HealthTracker:          env.HealthTracker,
		HistoryRecorder:        env.HistoryRecorder,
		RateLimiter:            env.RateLimiter,
		EffectivePollTimeout:   env.EffectivePollTimeout,
		EffectiveStreamTimeout: env.EffectiveStreamTimeout,
	}
}
func (s *SendMessageTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name: "send_message",
		Description: toolDescription(
			`Send a message to a connected A2A agent by alias or URL.`,
			`Use 'message' for simple plain-text messages.`,
			`Use 'parts' when you need to send structured data (JSON objects), URLs, or multi-part content.`,
			`Parts also support base64-encoded binary data via the 'raw' field.`,
			`If both are provided, 'parts' takes precedence.`,
		),
		OutputSchema: sendMessageOutputSchema(),
	}
}

// sendRequest holds the validated and resolved parameters needed to send a message.
type sendRequest struct {
	resolved *ResolveResult
	client   *a2aclient.Client
	request  *a2a.SendMessageRequest
}

// Handle sends a message to a connected A2A agent by alias or URL.
func (s *SendMessageTool) Handle(ctx context.Context, req *mcp.CallToolRequest, input *SendMessageInput) (*mcp.CallToolResult, *SendMessageOutput, error) {
	sr, err := s.buildSendRequest(ctx, input)
	if err != nil {
		return nil, nil, err
	}

	// Route to streaming or direct path.
	if s.AgentRegistry.SupportsStreaming(sr.resolved) {
		return s.sendViaStreaming(ctx, req, input, sr)
	}
	return s.sendDirect(ctx, input, sr)
}

// buildSendRequest validates input, resolves the agent, checks rate limits,
// builds the A2A message, and resolves the SDK client. Returns an error result
// if any step fails.
func (s *SendMessageTool) buildSendRequest(ctx context.Context, input *SendMessageInput) (*sendRequest, error) {
	if input.Agent == "" {
		return nil, errors.New("agent identifier is required")
	}

	contentParts, err := buildMessageParts(input.Message, input.Parts)
	if err != nil {
		return nil, err
	}

	resolved, err := s.AgentRegistry.ResolveAgent(input.Agent)
	if err != nil {
		return nil, err
	}

	if resolved.IsAlias {
		if err := s.RateLimiter.CheckRateLimit(resolved.Alias); err != nil {
			return nil, fmt.Errorf("rate limited: %w", err)
		}
	}

	// Determine context_id priority: explicit > stored > empty (new conversation).
	contextID := input.ContextID
	if contextID == "" && resolved.IsAlias {
		contextID = s.ContextStore.Get(input.Agent)
	}

	msg := a2a.NewMessage(a2a.MessageRoleUser, contentParts...)
	if contextID != "" {
		msg.ContextID = contextID
	}
	if input.TaskID != "" {
		msg.TaskID = a2a.TaskID(input.TaskID)
	}

	sendReq := &a2a.SendMessageRequest{Message: msg}
	if len(input.Metadata) > 0 {
		sendReq.Metadata = input.Metadata
	}
	// Requirement: CAC-2.1 — inject caller card into outbound metadata
	sendReq.Metadata = s.CallerCardInjector.InjectCallerCard(sendReq.Metadata)

	a2aClient, err := s.A2AClientResolver.Resolve(ctx, resolved)
	if err != nil {
		return nil, handleA2AError(err)
	}

	return &sendRequest{resolved: resolved, client: a2aClient, request: sendReq}, nil
}

// sendViaStreaming handles the streaming transport path.
func (s *SendMessageTool) sendViaStreaming(ctx context.Context, req *mcp.CallToolRequest, input *SendMessageInput, sr *sendRequest) (*mcp.CallToolResult, *SendMessageOutput, error) {
	result, structured, err := handleStreamingMessage(ctx, req, s.ContextStore, sr.client, sr.request, sr.resolved, input.Agent, s.effectiveStreamTimeout(input.PollTimeoutSeconds))
	if err != nil {
		s.recordHealthOutcome(sr.resolved, err)
		s.recordHistory(ctx, input, nil, nil)
		return nil, nil, err
	}

	if sr.resolved.IsAlias {
		s.HealthTracker.RecordSuccess(sr.resolved.Alias)
	}
	s.recordHistory(ctx, input, result, structured)
	return result, structured, nil
}

// sendDirect handles the non-streaming (request/response) transport path.
func (s *SendMessageTool) sendDirect(ctx context.Context, input *SendMessageInput, sr *sendRequest) (*mcp.CallToolResult, *SendMessageOutput, error) {
	response, err := sr.client.SendMessage(ctx, sr.request)
	if err != nil {
		s.recordHealthOutcome(sr.resolved, err)
		s.recordHistory(ctx, input, nil, nil)
		return nil, nil, handleA2AError(err)
	}

	if sr.resolved.IsAlias {
		s.HealthTracker.RecordSuccess(sr.resolved.Alias)
	}

	return s.processResponse(ctx, input, sr, response)
}

// processResponse handles the A2A response type switch for the direct path.
func (s *SendMessageTool) processResponse(ctx context.Context, input *SendMessageInput, sr *sendRequest, response a2a.SendMessageResult) (*mcp.CallToolResult, *SendMessageOutput, error) {
	switch v := response.(type) {
	case *a2a.Task:
		pollTimeout := s.EffectivePollTimeout(input.PollTimeoutSeconds)
		taskResult, meta, taskErr := s.handleTaskResult(ctx, sr.client, v, sr.resolved, input.Agent, pollTimeout)
		s.recordHistory(ctx, input, taskResult, meta)
		return taskResult, meta, taskErr
	case *a2a.Message:
		if sr.resolved.IsAlias && v.ContextID != "" {
			s.ContextStore.Set(input.Agent, v.ContextID)
		}
		structured := formatMessageResponse(v)
		s.recordHistory(ctx, input, nil, structured)
		return nil, structured, nil
	default:
		err := errors.New("unrecognized response format: expected Task or Message")
		s.recordHistory(ctx, input, nil, nil)
		return nil, nil, err
	}
}

// recordHealthOutcome classifies an error and records health state.
func (s *SendMessageTool) recordHealthOutcome(resolved *ResolveResult, err error) {
	if !resolved.IsAlias {
		return
	}
	outcome := health.ClassifyError(err)
	if outcome == health.OutcomeConnectionError {
		s.HealthTracker.RecordFailure(resolved.Alias)
	}
}

// recordHistory records an interaction to the history backend.
func (s *SendMessageTool) recordHistory(ctx context.Context, input *SendMessageInput, result *mcp.CallToolResult, structured *SendMessageOutput) {
	isError := result != nil && result.IsError
	s.HistoryRecorder.Record(ctx, history.RecordInput{
		Alias:     input.Agent,
		Sent:      summarizeMessage(input.Message, input.Parts),
		Response:  extractResultText(result),
		ContextID: extractResponseContextID(structured),
		TaskID:    extractResponseTaskID(structured),
		IsError:   isError,
	})
}

// effectiveStreamTimeout returns the stream timeout to use for a request.
// Per-request PollTimeoutSeconds takes precedence over the server default.
// A negative value means no timeout (wait indefinitely).
func (s *SendMessageTool) effectiveStreamTimeout(requestSeconds *int) time.Duration {
	return s.EffectiveStreamTimeout(requestSeconds)
}

// extractResultText extracts the primary response text from a CallToolResult.
// It returns the text from the first TextContent item (which is always the
// human-readable response text now that metadata items have been removed).
func extractResultText(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}
	for _, content := range result.Content {
		tc, ok := content.(*mcp.TextContent)
		if !ok {
			continue
		}
		return tc.Text
	}
	return ""
}

// extractResponseContextID extracts the context_id from a *SendMessageResponse.
// It checks the Task and Message fields for a non-empty ContextID.
func extractResponseContextID(resp *SendMessageOutput) string {
	if resp == nil {
		return ""
	}
	if resp.Task != nil {
		return resp.Task.ContextID
	}
	if resp.Message != nil {
		return resp.Message.ContextID
	}
	return ""
}

// extractResponseTaskID extracts the task_id from a *SendMessageResponse.
// Only tasks have IDs; messages do not.
func extractResponseTaskID(resp *SendMessageOutput) string {
	if resp == nil {
		return ""
	}
	if resp.Task != nil {
		return string(resp.Task.ID)
	}
	return ""
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

const taskPollInterval = 2 * time.Second

// handleTaskResult processes a *a2a.Task result from SendMessage, handling
// all task states including polling for non-terminal states.
func (s *SendMessageTool) handleTaskResult(ctx context.Context, a2aClient *a2aclient.Client, task *a2a.Task, resolved *ResolveResult, agent string, pollTimeout time.Duration) (*mcp.CallToolResult, *SendMessageOutput, error) {
	switch task.Status.State {
	case a2a.TaskStateCompleted:
		if resolved.IsAlias && task.ContextID != "" {
			s.ContextStore.Set(agent, task.ContextID)
		}
		return nil, formatTaskResponse(task), nil

	case a2a.TaskStateInputRequired:
		if resolved.IsAlias && task.ContextID != "" {
			s.ContextStore.Set(agent, task.ContextID)
		}
		return nil, formatInputRequiredResponse(task), nil

	case a2a.TaskStateAuthRequired:
		if resolved.IsAlias && task.ContextID != "" {
			s.ContextStore.Set(agent, task.ContextID)
		}
		return nil, formatInterruptedResponse(task, "auth-required"), nil

	case a2a.TaskStateFailed:
		// Requirement: SRES-3.3 — failed task returns structured content with isError
		if resolved.IsAlias && task.ContextID != "" {
			s.ContextStore.Set(agent, task.ContextID)
		}
		failMsg := "task failed"
		if task.Status.Message != nil {
			text := extractStatusMessageText(task.Status.Message)
			if text != "" {
				failMsg = text
			}
		}
		return nil, &SendMessageOutput{Task: task}, errors.New(failMsg)

	case a2a.TaskStateCanceled:
		// Requirement: SRES-3.4 — canceled task returns structured content with isError
		if resolved.IsAlias && task.ContextID != "" {
			s.ContextStore.Set(agent, task.ContextID)
		}
		return nil, &SendMessageOutput{Task: task}, errors.New("task was canceled by the agent")

	case a2a.TaskStateWorking, a2a.TaskStateSubmitted:
		// Known non-terminal states — poll for completion if task has an ID.
		if task.ID == "" {
			if resolved.IsAlias && task.ContextID != "" {
				s.ContextStore.Set(agent, task.ContextID)
			}
			return nil, nil, fmt.Errorf("agent returned non-terminal state %q without a task ID; cannot poll for completion", task.Status.State)
		}

		// Poll for non-terminal states up to the configured timeout.
		polledTask, err := s.pollTaskCompletion(ctx, a2aClient, task.ID, pollTimeout)
		if err != nil {
			return nil, nil, err
		}

		// Requirement: SRES-1.5 — polled task returns structured content
		// Re-evaluate the terminal state after polling.
		if resolved.IsAlias && polledTask.ContextID != "" {
			s.ContextStore.Set(agent, polledTask.ContextID)
		}
		switch polledTask.Status.State {
		case a2a.TaskStateCompleted:
			return nil, formatTaskResponse(polledTask), nil
		case a2a.TaskStateInputRequired:
			return nil, formatInputRequiredResponse(polledTask), nil
		case a2a.TaskStateAuthRequired:
			return nil, formatInterruptedResponse(polledTask, "auth-required"), nil
		case a2a.TaskStateFailed:
			failMsg := "task failed"
			if polledTask.Status.Message != nil {
				text := extractStatusMessageText(polledTask.Status.Message)
				if text != "" {
					failMsg = text
				}
			}
			return nil, &SendMessageOutput{Task: polledTask}, errors.New(failMsg)
		case a2a.TaskStateCanceled:
			return nil, &SendMessageOutput{Task: polledTask}, errors.New("task was canceled by the agent")
		default:
			return nil, &SendMessageOutput{Task: polledTask}, errors.New(fmt.Sprintf("timeout waiting for task completion (state: %s)", polledTask.Status.State))
		}

	default:
		// Unrecognized task state — likely a v0.x agent or protocol mismatch.
		if resolved.IsAlias && task.ContextID != "" {
			s.ContextStore.Set(agent, task.ContextID)
		}
		return nil, nil, fmt.Errorf("agent returned unrecognized task state %q — ensure the agent supports A2A protocol v1.0 or later", task.Status.State)
	}
}

// pollTaskCompletion polls the agent for task status every 2s until a terminal
// state is reached or the given timeout elapses. A zero timeout means no deadline.
func (s *SendMessageTool) pollTaskCompletion(ctx context.Context, a2aClient *a2aclient.Client, taskID a2a.TaskID, timeout time.Duration) (*a2a.Task, error) {
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
