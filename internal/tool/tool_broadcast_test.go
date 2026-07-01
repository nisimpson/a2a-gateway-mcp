package tool

import (
	"context"
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

func newBroadcastTool(reg *mockRegistry, clientResolver *mockClientResolver) *BroadcastMessageTool {
	return &BroadcastMessageTool{
		AgentRegistry:      reg,
		A2AClientResolver:  clientResolver,
		CallerCardInjector: &mockCallerCardInjector{},
		HealthTracker:      &mockHealthTracker{},
		HistoryRecorder:    &mockHistoryRecorder{},
		RateLimiter:        &mockRateLimiter{},
	}
}

func getTextContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("expected content, got none")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	return tc.Text
}

func TestBroadcast_NoAliases(t *testing.T) {
	b := newBroadcastTool(&mockRegistry{}, &mockClientResolver{})
	result, _, err := b.Handle(context.Background(), nil, &BroadcastMessageInput{
		Message: "hello",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if result != nil {
		t.Fatal("expected nil result for validation error")
	}
	if err.Error() != "at least one alias is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBroadcast_TooManyAliases(t *testing.T) {
	aliases := make([]string, 21)
	for i := range aliases {
		aliases[i] = "agent"
	}
	b := newBroadcastTool(&mockRegistry{}, &mockClientResolver{})
	result, _, err := b.Handle(context.Background(), nil, &BroadcastMessageInput{
		Aliases: aliases,
		Message: "hello",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if result != nil {
		t.Fatal("expected nil result for validation error")
	}
	if !contains(err.Error(), "maximum 20 aliases allowed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBroadcast_NoMessageOrParts(t *testing.T) {
	b := newBroadcastTool(&mockRegistry{}, &mockClientResolver{})
	result, _, err := b.Handle(context.Background(), nil, &BroadcastMessageInput{
		Aliases: []string{"agent1"},
	})
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

func TestBroadcast_InvalidTimeout(t *testing.T) {
	b := newBroadcastTool(&mockRegistry{}, &mockClientResolver{})

	timeout := 200
	result, _, err := b.Handle(context.Background(), nil, &BroadcastMessageInput{
		Aliases:        []string{"agent1"},
		Message:        "hello",
		TimeoutSeconds: &timeout,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if result != nil {
		t.Fatal("expected nil result for validation error")
	}
	if !contains(err.Error(), "timeout_seconds must be between") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBroadcast_SingleAgent_Success(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("broadcast reply"))
		writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
	}))
	defer agent.Close()

	reg := &mockRegistry{
		LookupFn: func(alias string) *registry.RegisteredAgent {
			return &registry.RegisteredAgent{Alias: alias, URL: agent.URL}
		},
	}

	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}

	b := newBroadcastTool(reg, clientResolver)
	result, out, err := b.Handle(context.Background(), nil, &BroadcastMessageInput{
		Aliases: []string{"agent1"},
		Message: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatalf("unexpected result for success: %v", result)
	}

	// Parse broadcast JSON output.
	if out == nil {
		t.Fatal("expected structured output for broadcast")
	}
	results := out
	agentResult, ok := results["agent1"]
	if !ok {
		t.Fatal("expected result for agent1")
	}
	if agentResult.Message == nil {
		t.Fatal("expected Message in structured output")
	}
}

func TestBroadcast_UnhealthySkipped(t *testing.T) {
	reg := &mockRegistry{
		LookupFn: func(alias string) *registry.RegisteredAgent {
			return &registry.RegisteredAgent{Alias: alias, URL: "http://example.com"}
		},
	}

	health := &mockHealthTracker{
		IsEnabledFn: func() bool { return true },
		IsHealthyFn: func(alias string) bool { return false },
	}

	b := newBroadcastTool(reg, &mockClientResolver{})
	b.HealthTracker = health

	result, out, err := b.Handle(context.Background(), nil, &BroadcastMessageInput{
		Aliases: []string{"sick-agent"},
		Message: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}

	if result != nil {
		t.Fatalf("unexpected result for success: %v", result)
	}
	if out == nil {
		t.Fatal("expected structured output for broadcast")
	}
	// When an agent is skipped, it won't be in the structured output
	// (only successful agents are included)
	if len(out) != 0 {
		t.Fatalf("expected empty output for skipped agent, got %v", out)
	}
}

func TestBroadcast_RateLimited(t *testing.T) {
	reg := &mockRegistry{
		LookupFn: func(alias string) *registry.RegisteredAgent {
			return &registry.RegisteredAgent{Alias: alias, URL: "http://example.com"}
		},
	}

	rateLimiter := &mockRateLimiter{
		AllowFn: func(alias string) bool { return false },
	}

	b := newBroadcastTool(reg, &mockClientResolver{})
	b.RateLimiter = rateLimiter

	result, out, err := b.Handle(context.Background(), nil, &BroadcastMessageInput{
		Aliases: []string{"limited-agent"},
		Message: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}

	if result != nil {
		t.Fatalf("unexpected result for success: %v", result)
	}
	if out == nil {
		t.Fatal("expected structured output for broadcast")
	}
	// When an agent is rate limited, it won't be in the structured output
	// (only successful agents are included)
	if len(out) != 0 {
		t.Fatalf("expected empty output for rate limited agent, got %v", out)
	}
}

func TestBroadcast_AgentNotFound(t *testing.T) {
	reg := &mockRegistry{
		LookupFn: func(alias string) *registry.RegisteredAgent { return nil },
	}

	b := newBroadcastTool(reg, &mockClientResolver{})
	result, out, err := b.Handle(context.Background(), nil, &BroadcastMessageInput{
		Aliases: []string{"unknown"},
		Message: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}

	if result != nil {
		t.Fatalf("unexpected result for success: %v", result)
	}
	if out == nil {
		t.Fatal("expected structured output for broadcast")
	}
	// When an agent is not found, it won't be in the structured output
	// (only successful agents are included)
	if len(out) != 0 {
		t.Fatalf("expected empty output for unknown agent, got %v", out)
	}
}

// Feature: server-options-context-propagation, Property 3: Broadcast async context propagation
// **Validates: Requirements 4.1, 4.2, 4.3**

func TestPropertyBroadcastAsyncContextPropagation(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for the number of context key-value pairs (1–5)
	numKeysGen := gen.IntRange(1, 5)

	properties.Property("async broadcast propagates context values and does not cancel derived context", prop.ForAll(
		func(numKeys int) bool {
			// Generate random key-value pairs for context
			keys := make([]contextKey, numKeys)
			values := make([]string, numKeys)
			for i := 0; i < numKeys; i++ {
				keys[i] = contextKey(fmt.Sprintf("broadcast-key-%d", i))
				values[i] = fmt.Sprintf("broadcast-value-%d-%d", i, time.Now().UnixNano())
			}

			// Set up an httptest server that responds with a message result
			agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				req, _ := readJSONRPCRequest(r)
				msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("broadcast async reply"))
				msg.ContextID = "broadcast-ctx"
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

			// Set up the registry to return the agent
			reg := &mockRegistry{
				LookupFn: func(alias string) *registry.RegisteredAgent {
					return &registry.RegisteredAgent{Alias: alias, URL: agent.URL}
				},
			}

			// Set up the client resolver to return our custom-transport client
			clientResolver := &mockClientResolver{
				ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
					return a2aClient, nil
				},
			}

			// Create a cancellable request context with values
			reqCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			for i, key := range keys {
				reqCtx = context.WithValue(reqCtx, key, values[i])
			}

			// Create the broadcast tool with an inbox to detect completion
			inbox := &mockInbox{}
			b := &BroadcastMessageTool{
				AgentRegistry:      reg,
				A2AClientResolver:  clientResolver,
				CallerCardInjector: &mockCallerCardInjector{},
				HealthTracker:      &mockHealthTracker{},
				HistoryRecorder:    &mockHistoryRecorder{},
				RateLimiter:        &mockRateLimiter{},
				Inbox:              inbox,
			}

			// Trigger the background broadcast (this is what broadcastAsync does for each alias)
			go b.backgroundBroadcastToAgent(reqCtx, "test-agent", &BroadcastMessageInput{
				Aliases: []string{"test-agent"},
				Message: "hello broadcast",
				Async:   boolPtr(true),
			}, 30)

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
