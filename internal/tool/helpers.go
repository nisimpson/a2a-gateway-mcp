package tool

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"log"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nisimpson/a2a-gateway-mcp/internal/specification"
	"github.com/nisimpson/a2a-gateway-mcp/registry"
)

// toolDescription constructs a tool description string by joining multiple lines
// with a space separator. This is useful for building multi-line descriptions
// that need to be collapsed into a single string for tool metadata.
func toolDescription(line string, lines ...string) string {
	lines = append([]string{line}, lines...)
	return strings.Join(lines, " ")
}

// sendMessageOutputSchema returns the JSON schema for send_message's structuredContent.
// It uses the embedded A2A specification schemas from internal/specification.
func sendMessageOutputSchema() *jsonschema.Schema {
	schema, err := specification.SendMessageResponseSchema()
	if err != nil {
		panic("sendMessageOutputSchema: " + err.Error())
	}
	return &schema
}

// broadcastMessageOutputSchema returns the JSON schema for broadcast_message's structuredContent.
// It describes a map of agent aliases to SendMessageResponse objects (which includes the async variant).
func broadcastMessageOutputSchema() *jsonschema.Schema {
	sendSchema, err := specification.SendMessageResponseSchema()
	if err != nil {
		panic("broadcastMessageOutputSchema: " + err.Error())
	}
	// The broadcast response is a map of alias → SendMessageResponse.
	// Each value is oneOf: message, task, or async dispatch result.
	return &jsonschema.Schema{
		Title:                "BroadcastMessageResponse",
		Type:                 "object",
		Description:          "map of agent aliases to per-agent A2A SendMessage responses",
		Definitions:          sendSchema.Definitions,
		AdditionalProperties: &jsonschema.Schema{OneOf: sendSchema.OneOf},
	}
}

// summarizeMessage returns a short text representation of a message for history recording.
// Uses the plain message string if available, otherwise summarizes parts.
func summarizeMessage(message string, parts []InputPart) string {
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

// extractTaskResponseText gets the human-readable text from a task's status message,
// falling back to artifact content.
func extractTaskResponseText(task *a2a.Task) string {
	if task.Status.Message != nil {
		if text := extractStatusMessageText(task.Status.Message); text != "" {
			return text
		}
	}
	return extractContentFromArtifacts(task.Artifacts)
}

// handleA2AError classifies an error returned by the a2aclient SDK.
// The MCP library will convert this to an error result.
func handleA2AError(err error) error {
	switch {
	case errors.Is(err, a2a.ErrTaskNotFound):
		return errors.New("task not found")
	case errors.Is(err, a2a.ErrTaskNotCancelable):
		return errors.New("task is not cancelable")
	default:
		return err
	}
}

// isTerminalOrInterrupted returns true for states that should stop stream consumption.
func isTerminalOrInterrupted(state a2a.TaskState) bool {
	switch state {
	case a2a.TaskStateCompleted, a2a.TaskStateFailed, a2a.TaskStateCanceled,
		a2a.TaskStateInputRequired, a2a.TaskStateAuthRequired, a2a.TaskStateRejected:
		return true
	}
	return false
}

// streamState accumulates streaming event data during consumption.
type streamState struct {
	// artifacts collects artifact parts in receipt order.
	artifacts []*a2a.Artifact

	// lastStatus holds the most recent status update (state + message).
	lastStatus *a2a.TaskStatus

	// taskID is populated from the first event that provides task info.
	taskID a2a.TaskID

	// contextID is populated from the first event that provides context info.
	contextID string

	// receivedEvents tracks whether any events were received (for error differentiation).
	receivedEvents bool

	// progressFn is an optional callback invoked for non-terminal status updates.
	// It receives the task state name and status message text. If nil, no
	// progress notifications are emitted.
	// Requirements: STRM-9.1, STRM-9.2
	progressFn func(state a2a.TaskState, message string)
}

// streamResult represents the terminal outcome of stream consumption.
type streamResult struct {
	// task is non-nil when a full *a2a.Task event terminates the stream.
	task *a2a.Task

	// message is non-nil when a *a2a.Message event terminates the stream.
	message *a2a.Message

	// terminatedByStatus is true when a TaskStatusUpdateEvent with
	// terminal/interrupted state terminates the stream (no full Task event).
	terminatedByStatus bool

	// state holds the accumulated stream state at termination.
	state *streamState
}

// buildTaskFromState constructs a synthetic *a2a.Task from accumulated streamState fields.
// Used when a terminal TaskStatusUpdateEvent terminates the stream without a full Task event.
func buildTaskFromState(state *streamState) *a2a.Task {
	task := &a2a.Task{
		ID:        state.taskID,
		ContextID: state.contextID,
		Artifacts: state.artifacts,
	}
	if state.lastStatus != nil {
		task.Status = *state.lastStatus
	}
	return task
}

// handleStreamingMessage calls SendStreamingMessage and consumes the event
// iterator until a terminal/interrupted state is reached or timeout expires.
// Returns structured output only (no MCP CallToolResult).
func handleStreamingMessage(
	ctx context.Context,
	req *mcp.CallToolRequest,
	store ContextStore,
	a2aClient *a2aclient.Client,
	sendReq *a2a.SendMessageRequest,
	resolved *registry.ResolveResult,
	agent string,
	timeout time.Duration,
) (*mcp.CallToolResult, *SendMessageOutput, error) {
	// Requirement: STRM-3.1, STRM-3.2, STRM-3.3 — enforce stream timeout.
	var streamCtx context.Context
	var cancel context.CancelFunc
	if timeout > 0 {
		streamCtx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		streamCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	// Requirement: STRM-2.1 — use the same request for streaming.
	events := a2aClient.SendStreamingMessage(streamCtx, sendReq)

	state := &streamState{}

	// Requirement: STRM-9.1, STRM-9.2 — wire progress notifications when token is present.
	if req != nil && req.Session != nil && req.Params != nil && req.Params.GetProgressToken() != nil {
		progressToken := req.Params.GetProgressToken()
		session := req.Session
		var progressCount float64
		state.progressFn = func(taskState a2a.TaskState, message string) {
			progressCount++
			notifyErr := session.NotifyProgress(streamCtx, &mcp.ProgressNotificationParams{
				ProgressToken: progressToken,
				Progress:      progressCount,
				Message:       fmt.Sprintf("%s: %s", taskState, message),
			})
			if notifyErr != nil {
				// Requirement: STRM-9.3 — log and continue.
				log.Printf("failed to emit progress notification: %v", notifyErr)
			}
		}
	}
	result, err := consumeStream(streamCtx, events, state)
	if err != nil {
		// Requirement: STRM-7.1, STRM-7.2 — return error as MCP error result.
		errMsg := err.Error()
		switch {
		case errors.Is(err, ErrStreamTimeout):
			errMsg = fmt.Sprintf("timeout waiting for streaming response to complete (timed out after %s)", timeout)
		case errors.Is(err, ErrStreamConnectionTimeout):
			errMsg = fmt.Sprintf("stream connection timed out after %s", timeout)
		}
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: errMsg}},
		}, nil, nil
	}

	// Determine the context_id to store (from the final result).
	var contextID string

	// Handle the stream result based on terminal condition type.
	var structured *SendMessageOutput

	// Requirement: SRES-5.1, SRES-5.2, SRES-5.3 — streaming produces same structured content format
	switch {
	case result.task != nil:
		// A full *a2a.Task event terminated the stream.
		contextID = result.task.ContextID
		structured = formatStreamTask(result.task)

	case result.message != nil:
		// A *a2a.Message event terminated the stream.
		contextID = result.message.ContextID
		structured = formatMessageResponse(result.message)

	case result.terminatedByStatus:
		// A terminal TaskStatusUpdateEvent terminated the stream.
		task := buildTaskFromState(result.state)
		contextID = task.ContextID
		structured = formatStreamTask(task)
	}

	// Requirement: STRM-4.1, STRM-4.4 — store context_id for alias-based agents.
	if contextID == "" {
		contextID = result.state.contextID
	}
	if resolved.IsAlias && contextID != "" {
		store.Set(agent, contextID)
	}

	return nil, structured, nil
}

// formatStreamTask applies the same task-state-based formatting as the
// polling path: completed → formatTaskResponse, input-required →
// formatInputRequiredResponse, auth-required → formatInterruptedResponse,
// failed/canceled/rejected → structured output with task.
func formatStreamTask(task *a2a.Task) *SendMessageOutput {
	switch task.Status.State {
	case a2a.TaskStateCompleted:
		return formatTaskResponse(task)

	case a2a.TaskStateInputRequired:
		return formatInputRequiredResponse(task)

	case a2a.TaskStateAuthRequired:
		return formatInterruptedResponse(task, "auth-required")

	case a2a.TaskStateFailed:
		return &SendMessageOutput{Task: task}

	case a2a.TaskStateCanceled:
		return &SendMessageOutput{Task: task}

	case a2a.TaskStateRejected:
		return &SendMessageOutput{Task: task}

	default:
		return &SendMessageOutput{Task: task}
	}
}

// consumeStream iterates over the event stream, accumulating state and
// returning when a terminal condition is reached.
//
// Error differentiation:
//   - If an error occurs before any events are received: "stream connection failed: <err>"
//   - If an error occurs after events have been received: "stream interrupted: <err>"
//
// Terminal conditions:
//   - *a2a.Task event: returns immediately with task in result
//   - *a2a.Message event: returns immediately with message in result
//   - TaskStatusUpdateEvent with terminal/interrupted state: returns with terminatedByStatus=true
//   - Iterator exhaustion without terminal event: returns error
func consumeStream(_ context.Context, events iter.Seq2[a2a.Event, error], state *streamState) (*streamResult, error) {
	for event, err := range events {
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				if state.receivedEvents {
					return nil, ErrStreamTimeout
				}
				return nil, ErrStreamConnectionTimeout
			}
			if state.receivedEvents {
				return nil, fmt.Errorf("stream interrupted: %w", err)
			}
			return nil, fmt.Errorf("stream connection failed: %w", err)
		}
		state.receivedEvents = true

		switch ev := event.(type) {
		case *a2a.Task:
			return &streamResult{task: ev, state: state}, nil

		case *a2a.Message:
			return &streamResult{message: ev, state: state}, nil

		case *a2a.TaskStatusUpdateEvent:
			info := ev.TaskInfo()
			if state.taskID == "" {
				state.taskID = info.TaskID
			}
			if state.contextID == "" && info.ContextID != "" {
				state.contextID = info.ContextID
			}
			state.lastStatus = &ev.Status

			if isTerminalOrInterrupted(ev.Status.State) {
				return &streamResult{terminatedByStatus: true, state: state}, nil
			}

			// Non-terminal: emit progress notification if callback is available.
			// Requirements: STRM-9.1, STRM-9.2, STRM-9.3
			if state.progressFn != nil {
				msg := ""
				if ev.Status.Message != nil {
					msg = extractStatusMessageText(ev.Status.Message)
				}
				emitProgress(state.progressFn, ev.Status.State, msg)
			}

		case *a2a.TaskArtifactUpdateEvent:
			info := ev.TaskInfo()
			if state.taskID == "" {
				state.taskID = info.TaskID
			}
			if state.contextID == "" && info.ContextID != "" {
				state.contextID = info.ContextID
			}
			state.artifacts = append(state.artifacts, ev.Artifact)

		default:
			// Unrecognized event type: skip defensively
		}
	}

	// Iterator exhausted without terminal event
	return nil, fmt.Errorf("stream ended without terminal event")
}

// emitProgress safely invokes the progress callback, recovering from panics
// to ensure stream processing is never interrupted by notification failures.
// Requirement: STRM-9.3 — failure does not abort the stream.
func emitProgress(fn func(a2a.TaskState, string), state a2a.TaskState, message string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("progress notification panic: %v", r)
		}
	}()
	fn(state, message)
}

// renderPart converts an a2a.Part to its string representation.
// Returns the rendered string and true if the part was successfully rendered,
// or empty string and false if the part is nil or has an unrecognized content type.
func renderPart(part *a2a.Part) (string, bool) {
	if part == nil {
		return "", false
	}
	switch v := part.Content.(type) {
	case a2a.Text:
		return string(v), true
	case a2a.Data:
		data, err := json.Marshal(v.Value)
		if err != nil {
			return fmt.Sprintf("[unserializable data: %v]", err), true
		}
		return string(data), true
	case a2a.URL:
		return string(v), true
	case a2a.Raw:
		return base64.StdEncoding.EncodeToString([]byte(v)), true
	default:
		return "", false
	}
}

// formatMessageResponse formats an a2a.Message response as structured output.
//
// Behavior:
//   - Renders all parts (Text, Data, URL, Raw) using renderPart, concatenated in order.
//   - If the message has zero parts, returns empty text.
//   - Metadata (context_id) is available via the structured *SendMessageOutput;
func formatMessageResponse(msg *a2a.Message) *SendMessageOutput {
	// Requirement: SRES-2.1, SRES-2.2 — return raw message as structured content
	return &SendMessageOutput{Message: msg}
}

// extractContentFromMessageParts renders all parts using renderPart and
// concatenates results with no separator. Nil parts are skipped.
func extractContentFromMessageParts(parts a2a.ContentParts) string {
	var texts []string

	for _, part := range parts {
		if rendered, ok := renderPart(part); ok {
			texts = append(texts, rendered)
		}
	}

	return strings.Join(texts, "")
}

// formatInterruptedResponse formats a response for a task in an interrupted
// state (e.g. input-required, auth-required). Returns structured output
// wrapping the task. It includes the agent's status message (explaining what
// input is needed) or artifact content if available.
func formatInterruptedResponse(task *a2a.Task, stateName string) *SendMessageOutput {
	// Requirement: SRES-3.1, SRES-3.2 — structured content for interrupted states
	return &SendMessageOutput{Task: task}
}

// formatInputRequiredResponse formats a response for a task in the
// input-required state. This is a convenience wrapper around
// formatInterruptedResponse.
func formatInputRequiredResponse(task *a2a.Task) *SendMessageOutput {
	// Requirement: SRES-3.1 — structured content for input-required state
	return formatInterruptedResponse(task, "input-required")
}

// formatTaskResponse extracts content from an A2A Task and formats it
// as structured output.
//
// Behavior:
//   - Renders all parts (Text, Data, URL, Raw) from artifacts using renderPart.
//   - Parts within the same artifact are concatenated with no separator.
//   - Content from different artifacts is separated by newline.
//   - If the task has no artifacts or artifacts with no parts, returns empty text.
func formatTaskResponse(task *a2a.Task) *SendMessageOutput {
	// Requirement: SRES-1.1, SRES-1.3, SRES-6.3 — return raw task as structured content
	return &SendMessageOutput{Task: task}
}

// extractContentFromArtifacts renders all parts from all artifacts using renderPart.
// Parts within the same artifact are concatenated with no separator.
// Content from different artifacts is separated by a newline character.
func extractContentFromArtifacts(artifacts []*a2a.Artifact) string {
	var artifactTexts []string

	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		var parts []string
		for _, part := range artifact.Parts {
			if rendered, ok := renderPart(part); ok {
				parts = append(parts, rendered)
			}
		}
		if len(parts) > 0 {
			artifactTexts = append(artifactTexts, strings.Join(parts, ""))
		}
	}

	return strings.Join(artifactTexts, "\n")
}
