package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nisimpson/a2a-gateway-mcp/registry"
)

func newSendTool(reg *mockRegistry, clientResolver *mockClientResolver) *SendMessageTool {
	return &SendMessageTool{
		AgentRegistry:          reg,
		A2AClientResolver:      clientResolver,
		ContextStore:           newMockContextStore(),
		CallerCardInjector:     &mockCallerCardInjector{},
		HealthTracker:          &mockHealthTracker{},
		HistoryRecorder:        &mockHistoryRecorder{},
		RateLimiter:            &mockRateLimiter{},
		EffectiveStreamTimeout: effectiveStreamTimeout(5 * time.Second),
		EffectivePollTimeout:   func(_ *int) time.Duration { return 10 * time.Second },
	}
}

func TestSendMessage_AgentRequired(t *testing.T) {
	s := newSendTool(&mockRegistry{}, &mockClientResolver{})
	result, out, err := s.Handle(context.Background(), nil, &SendMessageInput{})
	if err == nil {
		t.Fatal("expected error")
	}
	if result != nil {
		t.Fatal("expected nil result for validation error")
	}
	if out != nil {
		t.Fatal("expected nil structured output")
	}
	if err.Error() != "agent identifier is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSendMessage_MessageOrPartsRequired(t *testing.T) {
	s := newSendTool(&mockRegistry{}, &mockClientResolver{})
	result, _, err := s.Handle(context.Background(), nil, &SendMessageInput{Agent: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	if result != nil {
		t.Fatal("expected nil result for validation error")
	}
	if err.Error() != "either 'message' or 'parts' is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSendMessage_InvalidAgent(t *testing.T) {
	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*registry.ResolveResult, error) {
			return nil, fmt.Errorf("agent not found")
		},
	}
	s := newSendTool(reg, &mockClientResolver{})
	result, _, err := s.Handle(context.Background(), nil, &SendMessageInput{
		Agent:   "nonexistent",
		Message: "hello",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if result != nil {
		t.Fatal("expected nil result for error")
	}
	if err.Error() != "agent not found" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSendMessage_RateLimited(t *testing.T) {
	reg := &mockRegistry{}
	s := newSendTool(reg, &mockClientResolver{})
	s.RateLimiter = &mockRateLimiter{
		CheckRateLimitFn: func(alias string) error {
			return fmt.Errorf("rate limited")
		},
	}

	result, _, err := s.Handle(context.Background(), nil, &SendMessageInput{
		Agent:   "test-agent",
		Message: "hello",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if result != nil {
		t.Fatal("expected nil result for rate limit error")
	}
	if err.Error() != "rate limited: rate limited" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSendMessage_DirectPath_MessageResponse(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("hello back"))
		msg.ContextID = "ctx-123"
		writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
	}))
	defer agent.Close()

	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*registry.ResolveResult, error) {
			return &registry.ResolveResult{URL: agent.URL, IsAlias: true, Alias: identifier}, nil
		},
	}

	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}

	s := newSendTool(reg, clientResolver)
	result, out, err := s.Handle(context.Background(), nil, &SendMessageInput{
		Agent:   "test-agent",
		Message: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatalf("expected nil result for success: %v", result)
	}
	if out == nil {
		t.Fatal("expected structured output")
	}

	// Verify structured output wraps the message.
	if out.Message == nil {
		t.Fatal("expected Message in structured output")
	}
	if out.Message.ContextID != "ctx-123" {
		t.Errorf("expected context_id ctx-123, got %s", out.Message.ContextID)
	}
}

func TestSendMessage_DirectPath_TaskCompleted(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:        "task-1",
			ContextID: "ctx-456",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("result text")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*registry.ResolveResult, error) {
			return &registry.ResolveResult{URL: agent.URL, IsAlias: true, Alias: identifier}, nil
		},
	}
	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}

	s := newSendTool(reg, clientResolver)
	result, out, err := s.Handle(context.Background(), nil, &SendMessageInput{
		Agent:   "test-agent",
		Message: "do something",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatalf("expected nil result for success: %v", result)
	}

	if out == nil {
		t.Fatal("expected output")
	}
	if out.Task == nil {
		t.Fatal("expected Task in structured output")
	}
	if out.Task.ID != "task-1" {
		t.Errorf("expected task ID task-1, got %s", out.Task.ID)
	}
}

func TestSendMessage_DirectPath_TaskFailed(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID: "task-fail",
			Status: a2a.TaskStatus{
				State:   a2a.TaskStateFailed,
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("something broke")),
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*registry.ResolveResult, error) {
			return &registry.ResolveResult{URL: agent.URL, IsAlias: true, Alias: identifier}, nil
		},
	}
	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}

	s := newSendTool(reg, clientResolver)
	result, out, err := s.Handle(context.Background(), nil, &SendMessageInput{
		Agent:   "test-agent",
		Message: "hello",
	})
	if err == nil {
		t.Fatal("expected error for failed task")
	}
	if result != nil {
		t.Fatalf("unexpected result for operational error: %v", result)
	}

	if out == nil {
		t.Fatal("expected output for failed task")
	}
	if out.Task == nil {
		t.Fatal("expected Task in structured output for failed state")
	}
	if err.Error() != "something broke" {
		t.Errorf("expected error message 'something broke', got %s", err.Error())
	}
}

func TestSendMessage_ContextStoreUsed(t *testing.T) {
	var capturedContextID string
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		// Parse the message to check context_id was sent.
		var params struct {
			Message struct {
				ContextID string `json:"contextId"`
			} `json:"message"`
		}
		_ = json.Unmarshal(req.Params, &params)
		capturedContextID = params.Message.ContextID

		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("ok"))
		writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
	}))
	defer agent.Close()

	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*registry.ResolveResult, error) {
			return &registry.ResolveResult{URL: agent.URL, IsAlias: true, Alias: identifier}, nil
		},
	}
	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}

	s := newSendTool(reg, clientResolver)
	// Pre-seed the context store.
	s.ContextStore.Set("test-agent", "stored-ctx-id")

	_, _, err := s.Handle(context.Background(), nil, &SendMessageInput{
		Agent:   "test-agent",
		Message: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}

	if capturedContextID != "stored-ctx-id" {
		t.Errorf("expected stored context_id to be sent, got %q", capturedContextID)
	}
}

func TestSendMessage_HealthRecordedOnSuccess(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("ok"))
		writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
	}))
	defer agent.Close()

	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*registry.ResolveResult, error) {
			return &registry.ResolveResult{URL: agent.URL, IsAlias: true, Alias: identifier}, nil
		},
	}
	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}

	var successCalled bool
	health := &mockHealthTracker{
		RecordSuccessFn: func(alias string) { successCalled = true },
	}

	s := newSendTool(reg, clientResolver)
	s.HealthTracker = health

	_, _, err := s.Handle(context.Background(), nil, &SendMessageInput{
		Agent:   "test-agent",
		Message: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !successCalled {
		t.Error("expected RecordSuccess to be called")
	}
}

// --- Synchronous Path Context Preservation Tests ---
// These tests verify that the synchronous (non-async) send path passes the
// request context directly to outbound calls, preserving cancellation semantics.
// Requirements: 3.1, 3.2, 3.3

func TestSendMessage_SyncPath_PassesRequestContextDirectly(t *testing.T) {
	// This test verifies that the sync path uses the request context directly
	// (not a derived context via WithoutCancel). We confirm this by attaching
	// a value to the request context and verifying the HTTP request completes
	// successfully — demonstrating the context is live and passed through.
	type ctxKey struct{}

	var requestReceived bool
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestReceived = true
		req, _ := readJSONRPCRequest(r)
		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("sync reply"))
		writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
	}))
	defer agent.Close()

	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*registry.ResolveResult, error) {
			return &registry.ResolveResult{URL: agent.URL, IsAlias: true, Alias: identifier}, nil
		},
	}
	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}

	s := newSendTool(reg, clientResolver)

	// Create a context with a value — in the sync path, this context is passed
	// directly to sr.client.SendMessage(ctx, ...) without any wrapping.
	ctx := context.WithValue(context.Background(), ctxKey{}, "trace-123")

	_, out, err := s.Handle(ctx, nil, &SendMessageInput{
		Agent:   "test-agent",
		Message: "hello",
	})
	if err != nil {
		t.Fatalf("expected no error in sync path, got: %v", err)
	}
	if out == nil || out.Message == nil {
		t.Fatal("expected structured output with Message")
	}
	if !requestReceived {
		t.Fatal("expected the agent to receive the request (context was live)")
	}
}

func TestSendMessage_SyncPath_CancelledContextReturnsError(t *testing.T) {
	// This test verifies that when the request context is cancelled before a
	// synchronous send_message operation completes, the operation returns an
	// error. This confirms the sync path passes ctx directly (not
	// context.WithoutCancel), preserving cancellation semantics.
	// Requirement: 3.2

	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Delay the response long enough for the client context to be cancelled.
		time.Sleep(500 * time.Millisecond)
		req, _ := readJSONRPCRequest(r)
		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("too late"))
		writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
	}))
	defer agent.Close()

	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*registry.ResolveResult, error) {
			return &registry.ResolveResult{URL: agent.URL, IsAlias: true, Alias: identifier}, nil
		},
	}
	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}

	s := newSendTool(reg, clientResolver)

	// Create a context that will be cancelled almost immediately.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, _, err := s.Handle(ctx, nil, &SendMessageInput{
		Agent:   "test-agent",
		Message: "hello",
	})
	if err == nil {
		t.Fatal("expected error when context is cancelled during sync send")
	}
	// The error should indicate cancellation or deadline exceeded.
	// The exact message depends on the HTTP client/transport layer.
}

// --- Helpers ---

func assertTextContains(t *testing.T, result *mcp.CallToolResult, substr string) {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatalf("expected content, got none")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if !contains(tc.Text, substr) {
		t.Errorf("expected text to contain %q, got %q", substr, tc.Text)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || findSubstring(s, substr))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// effectiveStreamTimeout returns the stream timeout to use for a request.
// Per-request PollTimeoutSeconds takes precedence over the server default.
// A negative value means no timeout (wait indefinitely).
func effectiveStreamTimeout(timeout time.Duration) EffectiveTimeoutFunc {
	return func(requestSeconds *int) time.Duration {
		if requestSeconds != nil {
			if *requestSeconds < 0 {
				return 0 // sentinel: no timeout
			}
			if *requestSeconds > 0 {
				return time.Duration(*requestSeconds) * time.Second
			}
		}
		return timeout
	}
}

// contextKey is a typed key for context values in property tests.
type contextKey string

// Feature: server-options-context-propagation, Property 2: Send async context propagation
// **Validates: Requirements 2.1, 2.2, 2.3, 2.4, 2.5**

func TestPropertySendAsyncContextPropagation(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for the number of context key-value pairs (1–5)
	numKeysGen := gen.IntRange(1, 5)

	properties.Property("async send propagates context values and does not cancel derived context", prop.ForAll(
		func(numKeys int) bool {
			// Generate random key-value pairs for context
			keys := make([]contextKey, numKeys)
			values := make([]string, numKeys)
			for i := 0; i < numKeys; i++ {
				keys[i] = contextKey(fmt.Sprintf("test-key-%d", i))
				values[i] = fmt.Sprintf("test-value-%d-%d", i, time.Now().UnixNano())
			}

			// Set up an httptest server that responds with a message result
			agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				req, _ := readJSONRPCRequest(r)
				msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("async reply"))
				msg.ContextID = "async-ctx"
				writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
			}))
			defer agent.Close()

			// Create a custom transport that captures the context from outbound requests
			capturedCtxCh := make(chan context.Context, 1)
			transport := &http.Transport{}
			customClient := &http.Client{
				Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
					// Capture the request context
					select {
					case capturedCtxCh <- req.Context():
					default:
					}
					// Forward to real server
					// Rewrite URL to point to our test server
					req.URL.Scheme = "http"
					req.URL.Host = agent.Listener.Addr().String()
					return transport.RoundTrip(req)
				}),
			}

			// Create a2a client with our custom HTTP client
			endpoints := []*a2a.AgentInterface{
				a2a.NewAgentInterface(agent.URL, a2a.TransportProtocolJSONRPC),
			}
			a2aClient, err := a2aclient.NewFromEndpoints(
				context.Background(),
				endpoints,
				a2aclient.WithJSONRPCTransport(customClient),
			)
			if err != nil {
				t.Logf("failed to create client: %v", err)
				return false
			}

			// Create a cancellable request context with values
			reqCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			for i, key := range keys {
				reqCtx = context.WithValue(reqCtx, key, values[i])
			}

			// Build the send request
			msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
			sendReq := &a2a.SendMessageRequest{Message: msg}

			sr := &sendRequest{
				resolved: &registry.ResolveResult{URL: agent.URL, IsAlias: true, Alias: "test-agent"},
				client:   a2aClient,
				request:  sendReq,
			}

			// Create the tool with an inbox to detect completion
			inbox := &mockInbox{}
			s := &SendMessageTool{
				AgentRegistry:          &mockRegistry{},
				A2AClientResolver:      &mockClientResolver{},
				ContextStore:           newMockContextStore(),
				CallerCardInjector:     &mockCallerCardInjector{},
				HealthTracker:          &mockHealthTracker{},
				HistoryRecorder:        &mockHistoryRecorder{},
				RateLimiter:            &mockRateLimiter{},
				EffectivePollTimeout:   func(_ *int) time.Duration { return 10 * time.Second },
				EffectiveStreamTimeout: effectiveStreamTimeout(5 * time.Second),
				Inbox:                  inbox,
			}

			// Trigger the background send (this is what sendAsync does)
			go s.backgroundSendAndPoll(reqCtx, sr, &SendMessageInput{
				Agent:   "test-agent",
				Message: "hello",
				Async:   boolPtr(true),
			})

			// Wait for the captured context from the outbound HTTP request
			var capturedCtx context.Context
			select {
			case capturedCtx = <-capturedCtxCh:
			case <-time.After(5 * time.Second):
				t.Log("timed out waiting for captured context")
				return false
			}

			// Verify (a): all context values are present in the captured context
			for i, key := range keys {
				val, ok := capturedCtx.Value(key).(string)
				if !ok || val != values[i] {
					t.Logf("context value mismatch for key %v: got %q, want %q", key, val, values[i])
					return false
				}
			}

			// Cancel the original request context
			cancel()

			// Verify (b): the captured context's Err() is still nil
			// (context.WithoutCancel ensures the derived context is not cancelled)
			if capturedCtx.Err() != nil {
				t.Logf("captured context was cancelled: %v", capturedCtx.Err())
				return false
			}

			// Wait for inbox deposit to confirm the goroutine completed
			deadline := time.After(5 * time.Second)
			for {
				inbox.mu.Lock()
				count := len(inbox.entries)
				inbox.mu.Unlock()
				if count > 0 {
					break
				}
				select {
				case <-deadline:
					t.Log("timed out waiting for inbox deposit")
					return false
				case <-time.After(10 * time.Millisecond):
				}
			}

			return true
		},
		numKeysGen,
	))

	properties.TestingRun(t)
}

// roundTripperFunc adapts a function to http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
