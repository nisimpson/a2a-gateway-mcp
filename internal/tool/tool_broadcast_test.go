package tool

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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
		LookupFn: func(alias string) *AgentEntry {
			return &AgentEntry{Alias: alias, URL: agent.URL}
		},
	}

	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *ResolveResult) (*a2aclient.Client, error) {
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
		LookupFn: func(alias string) *AgentEntry {
			return &AgentEntry{Alias: alias, URL: "http://example.com"}
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
		LookupFn: func(alias string) *AgentEntry {
			return &AgentEntry{Alias: alias, URL: "http://example.com"}
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
		LookupFn: func(alias string) *AgentEntry { return nil },
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
