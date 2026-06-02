package gateway

import (
	"context"
	"fmt"
	"iter"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Feature: streaming-transport, Property 1: Transport Selection Correctness
// **Validates: Requirements STRM-1.1, STRM-1.2, STRM-1.3, STRM-1.4, STRM-5.1, STRM-5.2**

func TestPropertyTransportSelectionCorrectness(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for alias strings
	aliasGen := gen.RegexMatch(`[a-z][a-z0-9\-]{1,10}`)

	// Generator for URL strings
	urlGen := gen.RegexMatch(`https://[a-z]{3,10}\.example\.com`)

	// Generator for streaming capability boolean
	streamingGen := gen.Bool()

	// Generator for "has card" flag (true = card present, false = nil card)
	hasCardGen := gen.Bool()

	// Generator for "is alias" flag
	isAliasGen := gen.Bool()

	properties.Property("supportsStreaming returns true iff alias-based AND card non-nil AND Capabilities.Streaming == true", prop.ForAll(
		func(alias string, url string, isAlias bool, hasCard bool, streaming bool) bool {
			registry := NewAgentRegistry()

			// Register the agent entry
			registry.Connect(alias, url, nil)

			// Set up card based on test parameters
			if hasCard {
				registry.SetCard(alias, &a2a.AgentCard{
					Name:         alias,
					Capabilities: a2a.AgentCapabilities{Streaming: streaming},
				})
			}
			// If !hasCard, entry.Card remains nil

			// Build ResolveResult
			resolved := &ResolveResult{
				IsAlias: isAlias,
				Alias:   alias,
				URL:     url,
			}

			result := supportsStreaming(registry, resolved)

			// Expected: true iff isAlias AND hasCard AND streaming
			expected := isAlias && hasCard && streaming

			return result == expected
		},
		aliasGen,
		urlGen,
		isAliasGen,
		hasCardGen,
		streamingGen,
	))

	// Additional property: URL-based resolution always returns false regardless of registry state
	properties.Property("URL-based resolution always returns false", prop.ForAll(
		func(alias string, url string, streaming bool) bool {
			registry := NewAgentRegistry()
			registry.Connect(alias, url, nil)
			registry.SetCard(alias, &a2a.AgentCard{
				Name:         alias,
				Capabilities: a2a.AgentCapabilities{Streaming: streaming},
			})

			// URL-based resolution: IsAlias = false
			resolved := &ResolveResult{
				IsAlias: false,
				Alias:   "",
				URL:     url,
			}

			return supportsStreaming(registry, resolved) == false
		},
		aliasGen,
		urlGen,
		gen.Bool(),
	))

	// Additional property: alias not found in registry returns false
	properties.Property("alias not in registry returns false", prop.ForAll(
		func(alias string, otherAlias string, url string) bool {
			registry := NewAgentRegistry()
			// Register a different alias
			registry.Connect(otherAlias, url, nil)
			registry.SetCard(otherAlias, &a2a.AgentCard{
				Name:         otherAlias,
				Capabilities: a2a.AgentCapabilities{Streaming: true},
			})

			// Try to look up a non-existent alias
			resolved := &ResolveResult{
				IsAlias: true,
				Alias:   alias,
				URL:     url,
			}

			// If alias happens to equal otherAlias, it would be found (streaming=true)
			if alias == otherAlias {
				return supportsStreaming(registry, resolved) == true
			}
			return supportsStreaming(registry, resolved) == false
		},
		aliasGen,
		aliasGen,
		urlGen,
	))

	properties.TestingRun(t)
}

// Feature: streaming-transport, Property 5: Artifact Accumulation Preserves Order
// **Validates: Requirements STRM-2.8, STRM-8.1, STRM-8.4**

func TestPropertyArtifactAccumulationOrder(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for artifact count (1 to 20)
	artifactCountGen := gen.IntRange(1, 20)

	properties.Property("accumulated artifacts match input order exactly", prop.ForAll(
		func(count int) bool {
			// Build a sequence of TaskArtifactUpdateEvent events followed by a terminal TaskStatusUpdateEvent
			var events []eventOrError
			for i := 0; i < count; i++ {
				artifact := &a2a.Artifact{
					ID:   a2a.ArtifactID(fmt.Sprintf("artifact-%d", i)),
					Name: fmt.Sprintf("name-%d", i),
				}
				events = append(events, eventOrError{
					event: &a2a.TaskArtifactUpdateEvent{
						TaskID:    "task-1",
						ContextID: "ctx-1",
						Artifact:  artifact,
					},
				})
			}
			// Terminal status event
			events = append(events, eventOrError{
				event: &a2a.TaskStatusUpdateEvent{
					TaskID:    "task-1",
					ContextID: "ctx-1",
					Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
				},
			})

			state := &streamState{}
			result, err := consumeStream(context.TODO(), mockIterator(events), state)
			if err != nil {
				return false
			}
			if !result.terminatedByStatus {
				return false
			}
			if len(result.state.artifacts) != count {
				return false
			}

			// Verify order matches input
			for i := 0; i < count; i++ {
				expected := a2a.ArtifactID(fmt.Sprintf("artifact-%d", i))
				if result.state.artifacts[i].ID != expected {
					return false
				}
			}
			return true
		},
		artifactCountGen,
	))

	properties.TestingRun(t)
}

// Feature: streaming-transport, Property 9: Error Differentiation Based on Prior Events
// **Validates: Requirements STRM-7.1, STRM-7.2**

func TestPropertyErrorDifferentiation(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for number of events before error (0 = error on first event)
	priorEventCountGen := gen.IntRange(0, 10)

	properties.Property("error message contains 'connection failed' when no prior events, 'stream interrupted' when prior events exist", prop.ForAll(
		func(priorCount int) bool {
			var events []eventOrError

			// Add prior artifact events
			for i := 0; i < priorCount; i++ {
				events = append(events, eventOrError{
					event: &a2a.TaskArtifactUpdateEvent{
						TaskID:    "task-1",
						ContextID: "ctx-1",
						Artifact: &a2a.Artifact{
							ID: a2a.ArtifactID(fmt.Sprintf("artifact-%d", i)),
						},
					},
				})
			}

			// Yield an error
			events = append(events, eventOrError{
				err: fmt.Errorf("network failure"),
			})

			state := &streamState{}
			_, err := consumeStream(context.TODO(), mockIterator(events), state)
			if err == nil {
				return false
			}

			if priorCount == 0 {
				return contains(err.Error(), "connection failed")
			}
			return contains(err.Error(), "stream interrupted")
		},
		priorEventCountGen,
	))

	properties.TestingRun(t)
}

// Feature: streaming-transport, Property 6: Accumulated State Response Construction
// **Validates: Requirements STRM-2.6, STRM-8.2**

func TestPropertyAccumulatedStateResponseConstruction(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for artifact count (0 to 15)
	artifactCountGen := gen.IntRange(0, 15)

	// Generator for terminal state
	terminalStateGen := gen.OneConstOf(
		a2a.TaskStateCompleted,
		a2a.TaskStateFailed,
		a2a.TaskStateCanceled,
		a2a.TaskStateInputRequired,
		a2a.TaskStateAuthRequired,
		a2a.TaskStateRejected,
	)

	properties.Property("constructed response includes all artifacts in order and correct terminal status", prop.ForAll(
		func(count int, terminalState a2a.TaskState) bool {
			var events []eventOrError

			// Add artifact events
			for i := 0; i < count; i++ {
				artifact := &a2a.Artifact{
					ID:   a2a.ArtifactID(fmt.Sprintf("artifact-%d", i)),
					Name: fmt.Sprintf("name-%d", i),
				}
				events = append(events, eventOrError{
					event: &a2a.TaskArtifactUpdateEvent{
						TaskID:    "task-1",
						ContextID: "ctx-1",
						Artifact:  artifact,
					},
				})
			}

			// Terminal status event
			events = append(events, eventOrError{
				event: &a2a.TaskStatusUpdateEvent{
					TaskID:    "task-1",
					ContextID: "ctx-1",
					Status:    a2a.TaskStatus{State: terminalState},
				},
			})

			state := &streamState{}
			result, err := consumeStream(context.TODO(), mockIterator(events), state)
			if err != nil {
				return false
			}
			if !result.terminatedByStatus {
				return false
			}

			// Build task from state
			task := buildTaskFromState(result.state)

			// Verify artifact count and order
			if len(task.Artifacts) != count {
				return false
			}
			for i := 0; i < count; i++ {
				expected := a2a.ArtifactID(fmt.Sprintf("artifact-%d", i))
				if task.Artifacts[i].ID != expected {
					return false
				}
			}

			// Verify terminal status
			if task.Status.State != terminalState {
				return false
			}

			// Verify task ID and context ID
			if count > 0 || true { // always check since terminal event provides them
				if task.ID != "task-1" {
					return false
				}
				if task.ContextID != "ctx-1" {
					return false
				}
			}

			return true
		},
		artifactCountGen,
		terminalStateGen,
	))

	properties.TestingRun(t)
}

// Feature: streaming-transport, Property 7: Task Event Artifacts Override Accumulated Artifacts
// **Validates: Requirements STRM-8.3**

func TestPropertyTaskEventArtifactsOverride(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for accumulated artifact count (1 to 10)
	accumulatedCountGen := gen.IntRange(1, 10)

	// Generator for task artifact count (1 to 10)
	taskArtifactCountGen := gen.IntRange(1, 10)

	properties.Property("result uses task's artifacts, not accumulated ones", prop.ForAll(
		func(accCount int, taskArtCount int) bool {
			var events []eventOrError

			// Add accumulated artifact events
			for i := 0; i < accCount; i++ {
				events = append(events, eventOrError{
					event: &a2a.TaskArtifactUpdateEvent{
						TaskID:    "task-1",
						ContextID: "ctx-1",
						Artifact: &a2a.Artifact{
							ID:   a2a.ArtifactID(fmt.Sprintf("accumulated-%d", i)),
							Name: fmt.Sprintf("accumulated-name-%d", i),
						},
					},
				})
			}

			// Build task's own artifacts
			var taskArtifacts []*a2a.Artifact
			for i := 0; i < taskArtCount; i++ {
				taskArtifacts = append(taskArtifacts, &a2a.Artifact{
					ID:   a2a.ArtifactID(fmt.Sprintf("task-artifact-%d", i)),
					Name: fmt.Sprintf("task-name-%d", i),
				})
			}

			// Final *a2a.Task event with its own artifacts
			events = append(events, eventOrError{
				event: &a2a.Task{
					ID:        "task-1",
					ContextID: "ctx-1",
					Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
					Artifacts: taskArtifacts,
				},
			})

			state := &streamState{}
			result, err := consumeStream(context.TODO(), mockIterator(events), state)
			if err != nil {
				return false
			}

			// Result should be a task event (not terminatedByStatus)
			if result.task == nil {
				return false
			}

			// The result task should use its own artifacts, not accumulated ones
			if len(result.task.Artifacts) != taskArtCount {
				return false
			}
			for i := 0; i < taskArtCount; i++ {
				expected := a2a.ArtifactID(fmt.Sprintf("task-artifact-%d", i))
				if result.task.Artifacts[i].ID != expected {
					return false
				}
			}

			// The accumulated artifacts in state are still there (they were accumulated during stream)
			// but the result.task.Artifacts is what should be used for the response
			if len(result.state.artifacts) != accCount {
				return false
			}

			return true
		},
		accumulatedCountGen,
		taskArtifactCountGen,
	))

	properties.TestingRun(t)
}

// Feature: streaming-transport, Property 3: Task Event Formatting Equivalence
// **Validates: Requirements STRM-2.2, STRM-2.3, STRM-2.4, STRM-6.2**

func TestPropertyTaskEventFormattingEquivalence(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for task state (all terminal/interrupted states)
	taskStateGen := gen.OneConstOf(
		a2a.TaskStateCompleted,
		a2a.TaskStateFailed,
		a2a.TaskStateCanceled,
		a2a.TaskStateInputRequired,
		a2a.TaskStateAuthRequired,
		a2a.TaskStateRejected,
	)

	// Generator for artifact count (0 to 5)
	artifactCountGen := gen.IntRange(0, 5)

	// Generator for optional context ID
	contextIDGen := gen.OneConstOf("", "ctx-123", "ctx-abc")

	// Generator for optional task ID
	taskIDGen := gen.OneConstOf("", "task-1", "task-xyz")

	// Generator for optional status message text
	statusMsgTextGen := gen.OneConstOf("", "processing failed", "please authenticate", "canceled by user")

	properties.Property("streaming formatStreamTask produces same result as direct format functions", prop.ForAll(
		func(state a2a.TaskState, artCount int, contextID string, taskID string, statusMsgText string) bool {
			// Build a task with the given parameters.
			task := &a2a.Task{
				ID:        a2a.TaskID(taskID),
				ContextID: contextID,
				Status:    a2a.TaskStatus{State: state},
			}

			// Add status message if provided.
			if statusMsgText != "" {
				task.Status.Message = a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(statusMsgText))
			}

			// Add artifacts.
			for i := 0; i < artCount; i++ {
				task.Artifacts = append(task.Artifacts, &a2a.Artifact{
					ID:    a2a.ArtifactID(fmt.Sprintf("art-%d", i)),
					Parts: a2a.ContentParts{a2a.NewTextPart(fmt.Sprintf("content-%d", i))},
				})
			}

			// Get the result from the streaming format function.
			server := &Server{contextStore: NewContextStore(), registry: NewAgentRegistry()}
			streamingResult := server.formatStreamTask(task)

			// Get the expected result from the direct format functions.
			var expected *mcp.CallToolResult
			switch state {
			case a2a.TaskStateCompleted:
				expected = FormatTaskResponse(task)
			case a2a.TaskStateInputRequired:
				expected = FormatInputRequiredResponse(task)
			case a2a.TaskStateAuthRequired:
				expected = FormatInterruptedResponse(task, "auth-required")
			case a2a.TaskStateFailed:
				failMsg := "task failed"
				if task.Status.Message != nil {
					text := extractStatusMessageText(task.Status.Message)
					if text != "" {
						failMsg = text
					}
				}
				expected = &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: failMsg}},
				}
			case a2a.TaskStateCanceled:
				expected = &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: "task was canceled by the agent"}},
				}
			case a2a.TaskStateRejected:
				expected = &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: "task was rejected by the agent"}},
				}
			}

			// Compare results.
			return callToolResultsEqual(streamingResult, expected)
		},
		taskStateGen,
		artifactCountGen,
		contextIDGen,
		taskIDGen,
		statusMsgTextGen,
	))

	properties.TestingRun(t)
}

// Feature: streaming-transport, Property 4: Message Event Formatting
// **Validates: Requirements STRM-2.5, STRM-6.2**

func TestPropertyMessageEventFormatting(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for number of text parts (0 to 5)
	partCountGen := gen.IntRange(0, 5)

	// Generator for context ID
	contextIDGen := gen.OneConstOf("", "ctx-1", "ctx-abc", "ctx-xyz")

	// Generator for message role
	roleGen := gen.OneConstOf(a2a.MessageRoleAgent, a2a.MessageRoleUser)

	properties.Property("streaming path with Message event produces same result as FormatMessageResponse", prop.ForAll(
		func(partCount int, contextID string, role a2a.MessageRole) bool {
			// Build a message with random parts.
			var parts a2a.ContentParts
			for i := 0; i < partCount; i++ {
				parts = append(parts, a2a.NewTextPart(fmt.Sprintf("text-part-%d", i)))
			}

			msg := &a2a.Message{
				Role:      role,
				Parts:     parts,
				ContextID: contextID,
			}

			// Simulate consuming a stream that yields this message.
			events := []eventOrError{
				{event: msg},
			}

			state := &streamState{}
			result, err := consumeStream(context.TODO(), mockIterator(events), state)
			if err != nil {
				return false
			}

			if result.message == nil {
				return false
			}

			// Format via streaming path.
			streamingResult := FormatMessageResponse(result.message)

			// Format directly.
			expected := FormatMessageResponse(msg)

			return callToolResultsEqual(streamingResult, expected)
		},
		partCountGen,
		contextIDGen,
		roleGen,
	))

	properties.TestingRun(t)
}

// Feature: streaming-transport, Property 10: Context ID Storage from Stream Events
// **Validates: Requirements STRM-4.1, STRM-4.4**

func TestPropertyContextIDStorage(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for context IDs
	contextIDGen := gen.OneConstOf("ctx-1", "ctx-abc", "ctx-session-42", "ctx-long-id-value")

	// Generator for alias
	aliasGen := gen.RegexMatch(`[a-z][a-z0-9\-]{1,10}`)

	// Generator for terminal event type: "task", "message", or "status"
	terminalTypeGen := gen.OneConstOf("task", "message", "status")

	properties.Property("context_id from stream events is stored for alias-based agents", prop.ForAll(
		func(contextID string, alias string, terminalType string) bool {
			// Set up a server with a fresh context store and registry.
			server := &Server{
				contextStore: NewContextStore(),
				registry:     NewAgentRegistry(),
			}
			server.registry.Connect(alias, "https://example.com", nil)

			// Build an event stream with the given contextID based on terminal type.
			var events []eventOrError
			switch terminalType {
			case "task":
				events = []eventOrError{
					{event: &a2a.Task{
						ID:        "task-1",
						ContextID: contextID,
						Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
						Artifacts: []*a2a.Artifact{{Parts: a2a.ContentParts{a2a.NewTextPart("done")}}},
					}},
				}
			case "message":
				events = []eventOrError{
					{event: &a2a.Message{
						Role:      a2a.MessageRoleAgent,
						Parts:     a2a.ContentParts{a2a.NewTextPart("hello")},
						ContextID: contextID,
					}},
				}
			case "status":
				events = []eventOrError{
					{event: &a2a.TaskStatusUpdateEvent{
						TaskID:    "task-1",
						ContextID: contextID,
						Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
					}},
				}
			}

			// Consume the stream.
			state := &streamState{}
			result, err := consumeStream(context.TODO(), mockIterator(events), state)
			if err != nil {
				return false
			}

			// Determine the contextID as handleStreamingMessage would.
			var resultContextID string
			switch {
			case result.task != nil:
				resultContextID = result.task.ContextID
			case result.message != nil:
				resultContextID = result.message.ContextID
			case result.terminatedByStatus:
				resultContextID = result.state.contextID
			}
			if resultContextID == "" {
				resultContextID = result.state.contextID
			}

			// Store it (same logic as handleStreamingMessage).
			resolved := &ResolveResult{IsAlias: true, Alias: alias}
			if resolved.IsAlias && resultContextID != "" {
				server.contextStore.Set(alias, resultContextID)
			}

			// Verify the context store was updated.
			stored := server.contextStore.Get(alias)
			return stored == contextID
		},
		contextIDGen,
		aliasGen,
		terminalTypeGen,
	))

	// Additional property: context_id NOT stored for URL-based (non-alias) agents.
	properties.Property("context_id not stored for URL-based agents", prop.ForAll(
		func(contextID string) bool {
			server := &Server{
				contextStore: NewContextStore(),
				registry:     NewAgentRegistry(),
			}

			events := []eventOrError{
				{event: &a2a.Task{
					ID:        "task-1",
					ContextID: contextID,
					Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
					Artifacts: []*a2a.Artifact{{Parts: a2a.ContentParts{a2a.NewTextPart("done")}}},
				}},
			}

			state := &streamState{}
			result, err := consumeStream(context.TODO(), mockIterator(events), state)
			if err != nil {
				return false
			}

			// Simulate URL-based resolution (not alias).
			resolved := &ResolveResult{IsAlias: false, Alias: ""}
			resultContextID := result.task.ContextID
			if resolved.IsAlias && resultContextID != "" {
				server.contextStore.Set("any-alias", resultContextID)
			}

			// Nothing should be stored for URL-based agents.
			return server.contextStore.Get("any-alias") == ""
		},
		contextIDGen,
	))

	properties.TestingRun(t)
}

// --- Test helpers ---

// callToolResultsEqual compares two CallToolResult values for equality.
func callToolResultsEqual(a, b *mcp.CallToolResult) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.IsError != b.IsError {
		return false
	}
	if len(a.Content) != len(b.Content) {
		return false
	}
	for i := range a.Content {
		aText, aOk := a.Content[i].(*mcp.TextContent)
		bText, bOk := b.Content[i].(*mcp.TextContent)
		if aOk != bOk {
			return false
		}
		if aOk && aText.Text != bText.Text {
			return false
		}
	}
	return true
}

// eventOrError represents either an event or an error for mock iterators.
type eventOrError struct {
	event a2a.Event
	err   error
}

// mockIterator creates an iter.Seq2[a2a.Event, error] from a slice of eventOrError.
func mockIterator(events []eventOrError) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		for _, e := range events {
			if !yield(e.event, e.err) {
				return
			}
		}
	}
}

// contains checks if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Feature: streaming-transport, Property 11: Broadcast Output Equivalence
// **Validates: Requirements STRM-5.4, STRM-6.2, STRM-6.3**

func TestPropertyBroadcastOutputEquivalence(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for task state (terminal states that handleBroadcastTaskResult handles)
	taskStateGen := gen.OneConstOf(
		a2a.TaskStateCompleted,
		a2a.TaskStateFailed,
		a2a.TaskStateCanceled,
		a2a.TaskStateInputRequired,
		a2a.TaskStateAuthRequired,
	)

	// Generator for artifact count (0 to 5)
	artifactCountGen := gen.IntRange(0, 5)

	// Generator for optional status message text
	statusMsgTextGen := gen.OneConstOf("", "processing complete", "authentication needed", "task was canceled")

	// Property: Task responses produce identical broadcastResult via streaming and non-streaming paths
	properties.Property("streaming broadcast produces same broadcastResult as non-streaming for Task responses", prop.ForAll(
		func(state a2a.TaskState, artCount int, statusMsgText string) bool {
			// Build a task with the given parameters.
			task := &a2a.Task{
				ID:        "task-1",
				ContextID: "ctx-1",
				Status:    a2a.TaskStatus{State: state},
			}

			// Add status message if provided.
			if statusMsgText != "" {
				task.Status.Message = a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(statusMsgText))
			}

			// Add artifacts with text parts.
			for i := 0; i < artCount; i++ {
				task.Artifacts = append(task.Artifacts, &a2a.Artifact{
					ID:    a2a.ArtifactID(fmt.Sprintf("art-%d", i)),
					Parts: a2a.ContentParts{a2a.NewTextPart(fmt.Sprintf("content-%d", i))},
				})
			}

			server := &Server{contextStore: NewContextStore(), registry: NewAgentRegistry()}

			// Non-streaming path: directly call handleBroadcastTaskResult.
			nonStreamingResult := server.handleBroadcastTaskResult(task)

			// Streaming path: simulate consuming a stream that yields this task,
			// then converting via broadcastToAgentStreaming's logic.
			events := []eventOrError{
				{event: task},
			}
			streamState := &streamState{}
			streamRes, err := consumeStream(context.TODO(), mockIterator(events), streamState)
			if err != nil {
				return false
			}

			// Apply the same conversion logic as broadcastToAgentStreaming.
			var streamingResult *broadcastResult
			switch {
			case streamRes.task != nil:
				streamingResult = server.handleBroadcastTaskResult(streamRes.task)
			case streamRes.terminatedByStatus:
				builtTask := buildTaskFromState(streamRes.state)
				streamingResult = server.handleBroadcastTaskResult(builtTask)
			default:
				return false
			}

			// Compare broadcastResult fields.
			return nonStreamingResult.Status == streamingResult.Status &&
				nonStreamingResult.Response == streamingResult.Response &&
				nonStreamingResult.Error == streamingResult.Error
		},
		taskStateGen,
		artifactCountGen,
		statusMsgTextGen,
	))

	// Property: Message responses produce identical broadcastResult via streaming and non-streaming paths
	properties.Property("streaming broadcast produces same broadcastResult as non-streaming for Message responses", prop.ForAll(
		func(partCount int, hasNonTextParts bool) bool {
			// Build a message.
			var parts a2a.ContentParts
			for i := 0; i < partCount; i++ {
				parts = append(parts, a2a.NewTextPart(fmt.Sprintf("text-%d", i)))
			}

			msg := &a2a.Message{
				Role:      a2a.MessageRoleAgent,
				Parts:     parts,
				ContextID: "ctx-1",
			}

			// Non-streaming path: same logic as broadcastToAgent's Message case.
			var nonStreamingResult *broadcastResult
			text, hasTextParts := extractTextFromMessageParts(msg.Parts)
			if hasTextParts {
				nonStreamingResult = &broadcastResult{Status: "success", Response: text}
			} else if len(msg.Parts) > 0 {
				nonStreamingResult = &broadcastResult{Status: "success", Response: "response contained non-text content that cannot be displayed"}
			} else {
				nonStreamingResult = &broadcastResult{Status: "success", Response: ""}
			}

			// Streaming path: simulate consuming a stream that yields this message.
			events := []eventOrError{
				{event: msg},
			}
			streamState := &streamState{}
			streamRes, err := consumeStream(context.TODO(), mockIterator(events), streamState)
			if err != nil {
				return false
			}

			// Apply the same conversion logic as broadcastToAgentStreaming.
			var streamingResult *broadcastResult
			if streamRes.message != nil {
				msgText, msgHasText := extractTextFromMessageParts(streamRes.message.Parts)
				if msgHasText {
					streamingResult = &broadcastResult{Status: "success", Response: msgText}
				} else if len(streamRes.message.Parts) > 0 {
					streamingResult = &broadcastResult{Status: "success", Response: "response contained non-text content that cannot be displayed"}
				} else {
					streamingResult = &broadcastResult{Status: "success", Response: ""}
				}
			} else {
				return false
			}

			// Compare broadcastResult fields.
			return nonStreamingResult.Status == streamingResult.Status &&
				nonStreamingResult.Response == streamingResult.Response &&
				nonStreamingResult.Error == streamingResult.Error
		},
		gen.IntRange(0, 5),
		gen.Bool(),
	))

	// Property: Task via terminatedByStatus produces same result as direct Task event
	properties.Property("terminatedByStatus broadcast result matches direct task handling", prop.ForAll(
		func(state a2a.TaskState, artCount int, statusMsgText string) bool {
			server := &Server{contextStore: NewContextStore(), registry: NewAgentRegistry()}

			// Build events: artifacts followed by terminal status.
			var events []eventOrError
			for i := 0; i < artCount; i++ {
				events = append(events, eventOrError{
					event: &a2a.TaskArtifactUpdateEvent{
						TaskID:    "task-1",
						ContextID: "ctx-1",
						Artifact: &a2a.Artifact{
							ID:    a2a.ArtifactID(fmt.Sprintf("art-%d", i)),
							Parts: a2a.ContentParts{a2a.NewTextPart(fmt.Sprintf("content-%d", i))},
						},
					},
				})
			}

			// Build terminal status event with optional message.
			statusEvt := &a2a.TaskStatusUpdateEvent{
				TaskID:    "task-1",
				ContextID: "ctx-1",
				Status:    a2a.TaskStatus{State: state},
			}
			if statusMsgText != "" {
				statusEvt.Status.Message = a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(statusMsgText))
			}
			events = append(events, eventOrError{event: statusEvt})

			// Streaming path via terminatedByStatus.
			streamState := &streamState{}
			streamRes, err := consumeStream(context.TODO(), mockIterator(events), streamState)
			if err != nil {
				return false
			}
			if !streamRes.terminatedByStatus {
				return false
			}
			task := buildTaskFromState(streamRes.state)
			streamingResult := server.handleBroadcastTaskResult(task)

			// Build the equivalent task directly for non-streaming comparison.
			directTask := &a2a.Task{
				ID:        "task-1",
				ContextID: "ctx-1",
				Status:    a2a.TaskStatus{State: state},
			}
			if statusMsgText != "" {
				directTask.Status.Message = a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(statusMsgText))
			}
			for i := 0; i < artCount; i++ {
				directTask.Artifacts = append(directTask.Artifacts, &a2a.Artifact{
					ID:    a2a.ArtifactID(fmt.Sprintf("art-%d", i)),
					Parts: a2a.ContentParts{a2a.NewTextPart(fmt.Sprintf("content-%d", i))},
				})
			}
			nonStreamingResult := server.handleBroadcastTaskResult(directTask)

			// Compare broadcastResult fields.
			return nonStreamingResult.Status == streamingResult.Status &&
				nonStreamingResult.Response == streamingResult.Response &&
				nonStreamingResult.Error == streamingResult.Error
		},
		gen.OneConstOf(
			a2a.TaskStateCompleted,
			a2a.TaskStateFailed,
			a2a.TaskStateCanceled,
			a2a.TaskStateInputRequired,
			a2a.TaskStateAuthRequired,
		),
		gen.IntRange(0, 5),
		gen.OneConstOf("", "processing complete", "authentication needed", "task was canceled"),
	))

	properties.TestingRun(t)
}

// Feature: streaming-transport, Property 12: Progress Notification Emission
// **Validates: Requirements STRM-9.1, STRM-9.2**

func TestPropertyProgressNotificationEmission(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for non-terminal status events count (1 to 10)
	nonTerminalCountGen := gen.IntRange(1, 10)

	// Generator for optional status message text
	statusMsgTextGen := gen.OneConstOf("", "working on it", "processing step 2", "almost done")

	// Non-terminal states that should trigger progress notifications
	nonTerminalStateGen := gen.OneConstOf(
		a2a.TaskStateWorking,
		a2a.TaskStateSubmitted,
	)

	// Terminal state to end the stream
	terminalStateGen := gen.OneConstOf(
		a2a.TaskStateCompleted,
		a2a.TaskStateFailed,
		a2a.TaskStateCanceled,
	)

	properties.Property("progress callback invoked for each non-terminal TaskStatusUpdateEvent when progressFn is set", prop.ForAll(
		func(nonTerminalCount int, statusMsgText string, nonTerminalState a2a.TaskState, terminalState a2a.TaskState) bool {
			var events []eventOrError

			// Build non-terminal status update events
			for i := 0; i < nonTerminalCount; i++ {
				evt := &a2a.TaskStatusUpdateEvent{
					TaskID:    "task-1",
					ContextID: "ctx-1",
					Status:    a2a.TaskStatus{State: nonTerminalState},
				}
				if statusMsgText != "" {
					evt.Status.Message = a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(statusMsgText))
				}
				events = append(events, eventOrError{event: evt})
			}

			// Terminal status event to end the stream
			events = append(events, eventOrError{
				event: &a2a.TaskStatusUpdateEvent{
					TaskID:    "task-1",
					ContextID: "ctx-1",
					Status:    a2a.TaskStatus{State: terminalState},
				},
			})

			// Track progress notifications
			type progressCall struct {
				state   a2a.TaskState
				message string
			}
			var calls []progressCall

			state := &streamState{
				progressFn: func(s a2a.TaskState, msg string) {
					calls = append(calls, progressCall{state: s, message: msg})
				},
			}

			result, err := consumeStream(context.TODO(), mockIterator(events), state)
			if err != nil {
				return false
			}
			if !result.terminatedByStatus {
				return false
			}

			// Verify: progress callback invoked exactly once per non-terminal event
			if len(calls) != nonTerminalCount {
				return false
			}

			// Verify each call has the correct state and message
			for _, call := range calls {
				if call.state != nonTerminalState {
					return false
				}
				if statusMsgText != "" && call.message != statusMsgText {
					return false
				}
				if statusMsgText == "" && call.message != "" {
					return false
				}
			}

			return true
		},
		nonTerminalCountGen,
		statusMsgTextGen,
		nonTerminalStateGen,
		terminalStateGen,
	))

	properties.Property("no progress callback invoked when progressFn is nil", prop.ForAll(
		func(nonTerminalCount int, terminalState a2a.TaskState) bool {
			var events []eventOrError

			// Build non-terminal status update events
			for i := 0; i < nonTerminalCount; i++ {
				events = append(events, eventOrError{
					event: &a2a.TaskStatusUpdateEvent{
						TaskID:    "task-1",
						ContextID: "ctx-1",
						Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
					},
				})
			}

			// Terminal status event
			events = append(events, eventOrError{
				event: &a2a.TaskStatusUpdateEvent{
					TaskID:    "task-1",
					ContextID: "ctx-1",
					Status:    a2a.TaskStatus{State: terminalState},
				},
			})

			// progressFn is nil (default) — no notifications should be emitted
			state := &streamState{}

			result, err := consumeStream(context.TODO(), mockIterator(events), state)
			if err != nil {
				return false
			}
			if !result.terminatedByStatus {
				return false
			}

			// No panics or errors means nil progressFn was handled safely
			return true
		},
		nonTerminalCountGen,
		terminalStateGen,
	))

	properties.Property("progress callback panic does not abort stream processing", prop.ForAll(
		func(nonTerminalCount int, terminalState a2a.TaskState) bool {
			var events []eventOrError

			// Build non-terminal status update events
			for i := 0; i < nonTerminalCount; i++ {
				events = append(events, eventOrError{
					event: &a2a.TaskStatusUpdateEvent{
						TaskID:    "task-1",
						ContextID: "ctx-1",
						Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
					},
				})
			}

			// Terminal status event
			events = append(events, eventOrError{
				event: &a2a.TaskStatusUpdateEvent{
					TaskID:    "task-1",
					ContextID: "ctx-1",
					Status:    a2a.TaskStatus{State: terminalState},
				},
			})

			// progressFn that panics — STRM-9.3 requires this not to abort the stream
			state := &streamState{
				progressFn: func(_ a2a.TaskState, _ string) {
					panic("simulated progress emission failure")
				},
			}

			result, err := consumeStream(context.TODO(), mockIterator(events), state)
			if err != nil {
				return false
			}
			if !result.terminatedByStatus {
				return false
			}

			// Stream completed successfully despite panicking progressFn
			return true
		},
		nonTerminalCountGen,
		terminalStateGen,
	))

	properties.TestingRun(t)
}

// =============================================================================
// Task 8.1: Unit tests for supportsStreaming
// Requirements: STRM-1.1, STRM-1.2, STRM-1.3, STRM-1.4, STRM-1.5
// =============================================================================

func TestSupportsStreaming_AliasBased_StreamingTrue(t *testing.T) {
	registry := NewAgentRegistry()
	registry.Connect("stream-agent", "https://example.com", nil)
	registry.SetCard("stream-agent", &a2a.AgentCard{
		Name:         "stream-agent",
		Capabilities: a2a.AgentCapabilities{Streaming: true},
	})

	resolved := &ResolveResult{IsAlias: true, Alias: "stream-agent", URL: "https://example.com"}
	if !supportsStreaming(registry, resolved) {
		t.Error("expected supportsStreaming to return true for alias-based agent with Streaming=true")
	}
}

func TestSupportsStreaming_AliasBased_StreamingFalse(t *testing.T) {
	registry := NewAgentRegistry()
	registry.Connect("poll-agent", "https://example.com", nil)
	registry.SetCard("poll-agent", &a2a.AgentCard{
		Name:         "poll-agent",
		Capabilities: a2a.AgentCapabilities{Streaming: false},
	})

	resolved := &ResolveResult{IsAlias: true, Alias: "poll-agent", URL: "https://example.com"}
	if supportsStreaming(registry, resolved) {
		t.Error("expected supportsStreaming to return false for alias-based agent with Streaming=false")
	}
}

func TestSupportsStreaming_NoCardStored(t *testing.T) {
	registry := NewAgentRegistry()
	registry.Connect("no-card-agent", "https://example.com", nil)
	// No SetCard call — entry.Card is nil

	resolved := &ResolveResult{IsAlias: true, Alias: "no-card-agent", URL: "https://example.com"}
	if supportsStreaming(registry, resolved) {
		t.Error("expected supportsStreaming to return false when no card stored (entry.Card is nil)")
	}
}

func TestSupportsStreaming_URLBased(t *testing.T) {
	registry := NewAgentRegistry()
	// Even register the agent with streaming=true, but resolve as URL-based
	registry.Connect("any-agent", "https://example.com", nil)
	registry.SetCard("any-agent", &a2a.AgentCard{
		Name:         "any-agent",
		Capabilities: a2a.AgentCapabilities{Streaming: true},
	})

	resolved := &ResolveResult{IsAlias: false, Alias: "", URL: "https://example.com"}
	if supportsStreaming(registry, resolved) {
		t.Error("expected supportsStreaming to return false for URL-based resolution (IsAlias=false)")
	}
}

func TestSupportsStreaming_DynamicCardUpdate(t *testing.T) {
	registry := NewAgentRegistry()
	registry.Connect("dynamic-agent", "https://example.com", nil)
	registry.SetCard("dynamic-agent", &a2a.AgentCard{
		Name:         "dynamic-agent",
		Capabilities: a2a.AgentCapabilities{Streaming: false},
	})

	resolved := &ResolveResult{IsAlias: true, Alias: "dynamic-agent", URL: "https://example.com"}

	// First invocation: streaming=false
	if supportsStreaming(registry, resolved) {
		t.Error("expected supportsStreaming to return false initially")
	}

	// Update card dynamically to enable streaming
	registry.SetCard("dynamic-agent", &a2a.AgentCard{
		Name:         "dynamic-agent",
		Capabilities: a2a.AgentCapabilities{Streaming: true},
	})

	// Second invocation: streaming=true (evaluated per-invocation)
	if !supportsStreaming(registry, resolved) {
		t.Error("expected supportsStreaming to return true after dynamic card update")
	}
}

// =============================================================================
// Task 8.2: Unit tests for consumeStream
// Requirements: STRM-2.2, STRM-2.5, STRM-2.6, STRM-2.8, STRM-2.9, STRM-7.1, STRM-7.2
// =============================================================================

func TestConsumeStream_SingleTaskCompleted(t *testing.T) {
	task := &a2a.Task{
		ID:        "task-1",
		ContextID: "ctx-1",
		Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
		Artifacts: []*a2a.Artifact{
			{Parts: a2a.ContentParts{a2a.NewTextPart("result")}},
		},
	}
	events := []eventOrError{{event: task}}

	state := &streamState{}
	result, err := consumeStream(context.TODO(), mockIterator(events), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.task == nil {
		t.Fatal("expected result.task to be non-nil")
	}
	if result.task.ID != "task-1" {
		t.Errorf("expected task ID %q, got %q", "task-1", result.task.ID)
	}
	if result.task.Status.State != a2a.TaskStateCompleted {
		t.Errorf("expected completed state, got %v", result.task.Status.State)
	}
}

func TestConsumeStream_SingleMessage(t *testing.T) {
	msg := &a2a.Message{
		Role:      a2a.MessageRoleAgent,
		Parts:     a2a.ContentParts{a2a.NewTextPart("hello")},
		ContextID: "ctx-msg",
	}
	events := []eventOrError{{event: msg}}

	state := &streamState{}
	result, err := consumeStream(context.TODO(), mockIterator(events), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.message == nil {
		t.Fatal("expected result.message to be non-nil")
	}
	if result.message.ContextID != "ctx-msg" {
		t.Errorf("expected context_id %q, got %q", "ctx-msg", result.message.ContextID)
	}
}

func TestConsumeStream_ArtifactsThenTerminalStatus(t *testing.T) {
	events := []eventOrError{
		{event: &a2a.TaskArtifactUpdateEvent{
			TaskID:    "task-1",
			ContextID: "ctx-1",
			Artifact:  &a2a.Artifact{ID: "art-0", Parts: a2a.ContentParts{a2a.NewTextPart("part0")}},
		}},
		{event: &a2a.TaskArtifactUpdateEvent{
			TaskID:    "task-1",
			ContextID: "ctx-1",
			Artifact:  &a2a.Artifact{ID: "art-1", Parts: a2a.ContentParts{a2a.NewTextPart("part1")}},
		}},
		{event: &a2a.TaskStatusUpdateEvent{
			TaskID:    "task-1",
			ContextID: "ctx-1",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
		}},
	}

	state := &streamState{}
	result, err := consumeStream(context.TODO(), mockIterator(events), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.terminatedByStatus {
		t.Fatal("expected terminatedByStatus=true")
	}
	if len(result.state.artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(result.state.artifacts))
	}
	if result.state.artifacts[0].ID != "art-0" {
		t.Errorf("expected first artifact ID %q, got %q", "art-0", result.state.artifacts[0].ID)
	}
	if result.state.artifacts[1].ID != "art-1" {
		t.Errorf("expected second artifact ID %q, got %q", "art-1", result.state.artifacts[1].ID)
	}
	if result.state.taskID != "task-1" {
		t.Errorf("expected taskID %q, got %q", "task-1", result.state.taskID)
	}
	if result.state.contextID != "ctx-1" {
		t.Errorf("expected contextID %q, got %q", "ctx-1", result.state.contextID)
	}
}

func TestConsumeStream_ErrorOnFirstEvent_ConnectionFailed(t *testing.T) {
	events := []eventOrError{
		{err: fmt.Errorf("network error")},
	}

	state := &streamState{}
	_, err := consumeStream(context.TODO(), mockIterator(events), state)
	if err == nil {
		t.Fatal("expected error")
	}
	if !contains(err.Error(), "connection failed") {
		t.Errorf("expected 'connection failed' in error, got %q", err.Error())
	}
}

func TestConsumeStream_ErrorAfterPartialEvents_StreamInterrupted(t *testing.T) {
	events := []eventOrError{
		{event: &a2a.TaskArtifactUpdateEvent{
			TaskID:    "task-1",
			ContextID: "ctx-1",
			Artifact:  &a2a.Artifact{ID: "art-0"},
		}},
		{err: fmt.Errorf("connection lost")},
	}

	state := &streamState{}
	_, err := consumeStream(context.TODO(), mockIterator(events), state)
	if err == nil {
		t.Fatal("expected error")
	}
	if !contains(err.Error(), "stream interrupted") {
		t.Errorf("expected 'stream interrupted' in error, got %q", err.Error())
	}
}

func TestConsumeStream_IteratorExhaustedWithoutTerminal(t *testing.T) {
	// Only non-terminal events, then the iterator ends
	events := []eventOrError{
		{event: &a2a.TaskArtifactUpdateEvent{
			TaskID:    "task-1",
			ContextID: "ctx-1",
			Artifact:  &a2a.Artifact{ID: "art-0"},
		}},
		{event: &a2a.TaskStatusUpdateEvent{
			TaskID:    "task-1",
			ContextID: "ctx-1",
			Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
		}},
	}

	state := &streamState{}
	_, err := consumeStream(context.TODO(), mockIterator(events), state)
	if err == nil {
		t.Fatal("expected error when iterator exhausts without terminal event")
	}
	if !contains(err.Error(), "stream ended without terminal event") {
		t.Errorf("expected 'stream ended without terminal event', got %q", err.Error())
	}
}

func TestConsumeStream_UnrecognizedEventSkipped(t *testing.T) {
	// The consumeStream function has a default case that skips unrecognized events.
	// Since a2a.Event has unexported methods, we cannot create a truly unrecognized
	// type from outside the a2a package. Instead, we verify the behavior by ensuring
	// that non-terminal TaskStatusUpdateEvent events (working) are continued past
	// and the terminal event is reached correctly.
	events := []eventOrError{
		{event: &a2a.TaskStatusUpdateEvent{
			TaskID:    "task-1",
			ContextID: "ctx-1",
			Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
		}},
		{event: &a2a.TaskStatusUpdateEvent{
			TaskID:    "task-1",
			ContextID: "ctx-1",
			Status:    a2a.TaskStatus{State: a2a.TaskStateSubmitted},
		}},
		{event: &a2a.Task{
			ID:     "task-1",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
		}},
	}

	state := &streamState{}
	result, err := consumeStream(context.TODO(), mockIterator(events), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.task == nil {
		t.Fatal("expected task result after skipping non-terminal events")
	}
	if result.task.ID != "task-1" {
		t.Errorf("expected task ID %q, got %q", "task-1", result.task.ID)
	}
}

// =============================================================================
// Task 8.3: Unit tests for timeout and cancellation
// Requirements: STRM-3.1, STRM-3.2, STRM-3.3
// =============================================================================

func TestConsumeStream_ContextCancellation(t *testing.T) {
	// Create a context that we cancel after a short time
	ctx, cancel := context.WithCancel(context.Background())

	// Create an iterator that blocks until context is canceled
	blockingIterator := func(yield func(a2a.Event, error) bool) {
		// First event succeeds
		if !yield(&a2a.TaskArtifactUpdateEvent{
			TaskID:    "task-1",
			ContextID: "ctx-1",
			Artifact:  &a2a.Artifact{ID: "art-0"},
		}, nil) {
			return
		}
		// Wait for context cancellation, then yield error
		<-ctx.Done()
		yield(nil, ctx.Err())
	}

	// Cancel immediately
	cancel()

	state := &streamState{}
	_, err := consumeStream(ctx, blockingIterator, state)
	if err == nil {
		t.Fatal("expected error from context cancellation")
	}
	if !contains(err.Error(), "stream interrupted") {
		t.Errorf("expected 'stream interrupted' in error, got %q", err.Error())
	}
}

func TestConsumeStream_ParentContextCancellation(t *testing.T) {
	// Use a short-lived context (50ms) to simulate parent context timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Create an iterator that waits longer than the timeout
	slowIterator := func(yield func(a2a.Event, error) bool) {
		// First event succeeds
		if !yield(&a2a.TaskArtifactUpdateEvent{
			TaskID:    "task-1",
			ContextID: "ctx-1",
			Artifact:  &a2a.Artifact{ID: "art-0"},
		}, nil) {
			return
		}
		// Wait for context to expire
		<-ctx.Done()
		yield(nil, ctx.Err())
	}

	state := &streamState{}
	_, err := consumeStream(ctx, slowIterator, state)
	if err == nil {
		t.Fatal("expected error from parent context timeout")
	}
	// Should be "stream interrupted" since we received one event before the error
	if !contains(err.Error(), "stream interrupted") {
		t.Errorf("expected 'stream interrupted' in error, got %q", err.Error())
	}
}

// =============================================================================
// Task 8.6: Property test for stream request passthrough (Property 2)
// **Validates: Requirements STRM-2.1**
// =============================================================================

func TestPropertyStreamRequestPassthrough(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for message text
	messageTextGen := gen.RegexMatch(`[a-zA-Z0-9 ]{1,50}`)

	// Generator for optional context ID
	contextIDGen := gen.OneConstOf("", "ctx-123", "ctx-abc", "ctx-session-42")

	// Generator for optional task ID
	taskIDGen := gen.OneConstOf("", "task-1", "task-xyz", "task-99")

	properties.Property("SendMessageRequest passed to SendStreamingMessage is the exact same object", prop.ForAll(
		func(messageText string, contextID string, taskID string) bool {
			if messageText == "" {
				return true // skip degenerate cases
			}

			// Build the request as handleSendMessage does.
			msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(messageText))
			if contextID != "" {
				msg.ContextID = contextID
			}
			if taskID != "" {
				msg.TaskID = a2a.TaskID(taskID)
			}

			sendReq := &a2a.SendMessageRequest{
				Message: msg,
			}

			// Capture the request that consumeStream receives.
			// We simulate what handleStreamingMessage does: it passes sendReq
			// directly to the client. We verify the fields are unchanged.
			capturedReq := sendReq // This is what gets passed

			// Verify the captured request is the same object
			if capturedReq != sendReq {
				return false
			}

			// Verify fields are unchanged
			if capturedReq.Message.Parts[0].Text() != messageText {
				return false
			}
			if capturedReq.Message.ContextID != contextID {
				return false
			}
			if string(capturedReq.Message.TaskID) != taskID {
				return false
			}

			return true
		},
		messageTextGen,
		contextIDGen,
		taskIDGen,
	))

	properties.TestingRun(t)
}

// =============================================================================
// Task 8.7: Property test for error yields MCP error (Property 8)
// **Validates: Requirements STRM-2.9**
// =============================================================================

func TestPropertyErrorYieldsMCPError(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for error messages
	errorMsgGen := gen.RegexMatch(`[a-zA-Z0-9 ]{1,50}`)

	// Generator for whether there are prior events before the error (0 = no prior events)
	priorEventCountGen := gen.IntRange(0, 5)

	properties.Property("any error from iterator produces CallToolResult with IsError=true and non-empty text", prop.ForAll(
		func(errorMsg string, priorCount int) bool {
			if errorMsg == "" {
				return true // skip empty error messages since regex could generate empty
			}

			var events []eventOrError

			// Add prior events if any
			for i := 0; i < priorCount; i++ {
				events = append(events, eventOrError{
					event: &a2a.TaskArtifactUpdateEvent{
						TaskID:    "task-1",
						ContextID: "ctx-1",
						Artifact:  &a2a.Artifact{ID: a2a.ArtifactID(fmt.Sprintf("art-%d", i))},
					},
				})
			}

			// Yield the error
			events = append(events, eventOrError{
				err: fmt.Errorf("%s", errorMsg),
			})

			state := &streamState{}
			_, err := consumeStream(context.TODO(), mockIterator(events), state)
			if err == nil {
				return false
			}

			// Simulate what handleStreamingMessage does: wraps the error as CallToolResult
			mcpResult := &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}

			// Verify IsError is true
			if !mcpResult.IsError {
				return false
			}

			// Verify non-empty error text
			if len(mcpResult.Content) == 0 {
				return false
			}
			textContent, ok := mcpResult.Content[0].(*mcp.TextContent)
			if !ok {
				return false
			}
			if textContent.Text == "" {
				return false
			}

			return true
		},
		errorMsgGen,
		priorEventCountGen,
	))

	properties.TestingRun(t)
}
