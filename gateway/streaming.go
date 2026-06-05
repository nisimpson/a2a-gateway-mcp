package gateway

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// defaultStreamTimeout is the maximum duration the gateway will wait for an SSE
// stream to deliver a terminal or interrupted state before aborting.
const defaultStreamTimeout = 60 * time.Second

var (
	// ErrStreamTimeout is returned when a stream times out after receiving events.
	ErrStreamTimeout = errors.New("stream timeout")

	// ErrStreamConnectionTimeout is returned when a stream times out before receiving any events.
	ErrStreamConnectionTimeout = errors.New("stream connection timeout")
)

// effectiveStreamTimeout returns the stream timeout to use for a request.
// Per-request PollTimeoutSeconds takes precedence over the server default.
// A negative value means no timeout (wait indefinitely).
func (s *Server) effectiveStreamTimeout(requestSeconds *int) time.Duration {
	if requestSeconds != nil {
		if *requestSeconds < 0 {
			return 0 // sentinel: no timeout
		}
		if *requestSeconds > 0 {
			return time.Duration(*requestSeconds) * time.Second
		}
	}
	return s.streamTimeout
}

// supportsStreaming returns true if the resolved agent has a stored AgentCard
// with Capabilities.Streaming set to true. Returns false for URL-based
// resolution (no card lookup) or when no card is stored.
func supportsStreaming(registry *AgentRegistry, resolved *ResolveResult) bool {
	// URL-based resolution has no card lookup.
	if !resolved.IsAlias {
		return false
	}

	// Look up the registry entry for the alias.
	entry := registry.Lookup(resolved.Alias)
	if entry == nil {
		return false
	}

	// No card stored.
	if entry.Card == nil {
		return false
	}

	return entry.Card.Capabilities.Streaming
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
// It accumulates artifacts from TaskArtifactUpdateEvent events and constructs
// the final response using the same format functions as the polling path.
func (s *Server) handleStreamingMessage(
	ctx context.Context,
	req *mcp.CallToolRequest,
	a2aClient *a2aclient.Client,
	sendReq *a2a.SendMessageRequest,
	resolved *ResolveResult,
	agent string,
	timeout time.Duration,
) (*mcp.CallToolResult, error) {
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
		}, nil
	}

	// Determine the context_id to store (from the final result).
	var contextID string

	// Handle the stream result based on terminal condition type.
	var mcpResult *mcp.CallToolResult

	switch {
	case result.task != nil:
		// A full *a2a.Task event terminated the stream.
		contextID = result.task.ContextID
		mcpResult = s.formatStreamTask(result.task)

	case result.message != nil:
		// A *a2a.Message event terminated the stream.
		contextID = result.message.ContextID
		mcpResult = FormatMessageResponse(result.message)

	case result.terminatedByStatus:
		// A terminal TaskStatusUpdateEvent terminated the stream.
		task := buildTaskFromState(result.state)
		contextID = task.ContextID
		mcpResult = s.formatStreamTask(task)
	}

	// Requirement: STRM-4.1, STRM-4.4 — store context_id for alias-based agents.
	if contextID == "" {
		contextID = result.state.contextID
	}
	if resolved.IsAlias && contextID != "" {
		s.contextStore.Set(agent, contextID)
	}

	return mcpResult, nil
}

// formatStreamTask applies the same task-state-based formatting as the
// polling path: completed → FormatTaskResponse, input-required →
// FormatInputRequiredResponse, auth-required → FormatInterruptedResponse,
// failed/canceled/rejected → error result.
func (s *Server) formatStreamTask(task *a2a.Task) *mcp.CallToolResult {
	switch task.Status.State {
	case a2a.TaskStateCompleted:
		return FormatTaskResponse(task)

	case a2a.TaskStateInputRequired:
		return FormatInputRequiredResponse(task)

	case a2a.TaskStateAuthRequired:
		return FormatInterruptedResponse(task, "auth-required")

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
		}

	case a2a.TaskStateCanceled:
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "task was canceled by the agent"}},
		}

	case a2a.TaskStateRejected:
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "task was rejected by the agent"}},
		}

	default:
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("agent returned unrecognized task state %q — ensure the agent supports A2A protocol v1.0 or later", task.Status.State)}},
		}
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
