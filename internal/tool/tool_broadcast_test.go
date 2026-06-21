package tool

import (
	"context"
	"encoding/json"
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
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	assertTextContains(t, result, "at least one alias is required")
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
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	assertTextContains(t, result, "maximum 20 aliases allowed")
}

func TestBroadcast_NoMessageOrParts(t *testing.T) {
	b := newBroadcastTool(&mockRegistry{}, &mockClientResolver{})
	result, _, err := b.Handle(context.Background(), nil, &BroadcastMessageInput{
		Aliases: []string{"agent1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	assertTextContains(t, result, "either 'message' or 'parts' is required")
}

func TestBroadcast_InvalidTimeout(t *testing.T) {
	b := newBroadcastTool(&mockRegistry{}, &mockClientResolver{})

	timeout := 200
	result, _, err := b.Handle(context.Background(), nil, &BroadcastMessageInput{
		Aliases:        []string{"agent1"},
		Message:        "hello",
		TimeoutSeconds: &timeout,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	assertTextContains(t, result, "timeout_seconds must be between")
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
	result, _, err := b.Handle(context.Background(), nil, &BroadcastMessageInput{
		Aliases: []string{"agent1"},
		Message: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	// Parse broadcast JSON output.
	text := getTextContent(t, result)
	var results map[string]broadcastResult
	if err := json.Unmarshal([]byte(text), &results); err != nil {
		t.Fatalf("failed to parse broadcast result: %v", err)
	}
	agentResult, ok := results["agent1"]
	if !ok {
		t.Fatal("expected result for agent1")
	}
	if agentResult.Status != "success" {
		t.Errorf("expected status=success, got %s", agentResult.Status)
	}
	if !contains(agentResult.Response, "broadcast reply") {
		t.Errorf("expected response to contain 'broadcast reply', got %q", agentResult.Response)
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

	result, _, err := b.Handle(context.Background(), nil, &BroadcastMessageInput{
		Aliases: []string{"sick-agent"},
		Message: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}

	var results map[string]broadcastResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &results); err != nil {
		t.Fatalf("failed to parse broadcast result: %v", err)
	}
	agentResult, ok := results["sick-agent"]
	if !ok {
		t.Fatal("expected result for sick-agent")
	}
	if agentResult.Status != "skipped" {
		t.Errorf("expected status=skipped, got %s", agentResult.Status)
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

	result, _, err := b.Handle(context.Background(), nil, &BroadcastMessageInput{
		Aliases: []string{"limited-agent"},
		Message: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}

	var results map[string]broadcastResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &results); err != nil {
		t.Fatalf("failed to parse broadcast result: %v", err)
	}
	agentResult, ok := results["limited-agent"]
	if !ok {
		t.Fatal("expected result for limited-agent")
	}
	if agentResult.Status != "error" {
		t.Errorf("expected status=error, got %s", agentResult.Status)
	}
	if !contains(agentResult.Error, "rate limited") {
		t.Errorf("expected error to contain 'rate limited', got %q", agentResult.Error)
	}
}

func TestBroadcast_AgentNotFound(t *testing.T) {
	reg := &mockRegistry{
		LookupFn: func(alias string) *AgentEntry { return nil },
	}

	b := newBroadcastTool(reg, &mockClientResolver{})
	result, _, err := b.Handle(context.Background(), nil, &BroadcastMessageInput{
		Aliases: []string{"unknown"},
		Message: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}

	var results map[string]broadcastResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &results); err != nil {
		t.Fatalf("failed to parse broadcast result: %v", err)
	}
	agentResult, ok := results["unknown"]
	if !ok {
		t.Fatal("expected result for unknown")
	}
	if agentResult.Status != "error" {
		t.Errorf("expected status=error, got %s", agentResult.Status)
	}
	if !contains(agentResult.Error, "not registered") {
		t.Errorf("expected error to contain 'not registered', got %q", agentResult.Error)
	}
}
