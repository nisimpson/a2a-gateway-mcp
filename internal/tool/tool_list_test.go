package tool

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func newListTool() (*ListAgentsTool, *mockRegistry, *mockRateLimiter, *mockHealthTracker) {
	reg := &mockRegistry{}
	rl := &mockRateLimiter{}
	ht := &mockHealthTracker{
		GetStatusFn: func(alias string) string { return "healthy" },
	}
	tool := &ListAgentsTool{
		AgentRegistry: reg,
		RateLimiter:   rl,
		HealthTracker: ht,
	}
	return tool, reg, rl, ht
}

func TestList_Empty(t *testing.T) {
	tool, reg, _, _ := newListTool()
	reg.ListFn = func() []*AgentEntry { return nil }

	result, _, err := tool.Handle(context.Background(), nil, &ListAgentsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}

	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if tc.Text != "[]" {
		t.Errorf("expected empty JSON array '[]', got %q", tc.Text)
	}
}

func TestList_WithAgents(t *testing.T) {
	tool, reg, rl, ht := newListTool()

	reg.ListFn = func() []*AgentEntry {
		return []*AgentEntry{
			{Alias: "agent-one", URL: "http://one.example.com"},
			{Alias: "agent-two", URL: "http://two.example.com"},
		}
	}

	rl.GetFn = func(alias string) (float64, int, bool) {
		if alias == "agent-one" {
			return 5.0, 10, true
		}
		return 0, 0, false
	}

	ht.GetStatusFn = func(alias string) string { return "healthy" }
	ht.GetFailuresFn = func(alias string) (int, bool) { return 0, false }

	result, _, err := tool.Handle(context.Background(), nil, &ListAgentsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}

	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}

	var entries []ListAgentEntry
	if err := json.Unmarshal([]byte(tc.Text), &entries); err != nil {
		t.Fatalf("failed to unmarshal list response: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Alias != "agent-one" {
		t.Errorf("expected alias 'agent-one', got %q", entries[0].Alias)
	}
	if entries[0].RateLimit != "5.00 rps, burst 10" {
		t.Errorf("expected rate limit '5.00 rps, burst 10', got %q", entries[0].RateLimit)
	}
	if entries[1].RateLimit != "unlimited" {
		t.Errorf("expected 'unlimited' for agent-two, got %q", entries[1].RateLimit)
	}
	if entries[0].Health != "healthy" {
		t.Errorf("expected health 'healthy', got %q", entries[0].Health)
	}
}

func TestList_UnhealthyShowsFailures(t *testing.T) {
	tool, reg, rl, ht := newListTool()

	reg.ListFn = func() []*AgentEntry {
		return []*AgentEntry{
			{Alias: "sick-agent", URL: "http://sick.example.com"},
		}
	}

	rl.GetFn = func(alias string) (float64, int, bool) { return 0, 0, false }
	ht.GetStatusFn = func(alias string) string { return "unhealthy" }
	ht.GetFailuresFn = func(alias string) (int, bool) { return 5, true }

	result, _, err := tool.Handle(context.Background(), nil, &ListAgentsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}

	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}

	var entries []ListAgentEntry
	if err := json.Unmarshal([]byte(tc.Text), &entries); err != nil {
		t.Fatalf("failed to unmarshal list response: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Health != "unhealthy" {
		t.Errorf("expected health 'unhealthy', got %q", entries[0].Health)
	}
	if entries[0].ConsecutiveFailures == nil {
		t.Fatal("expected consecutive_failures to be set")
	}
	if *entries[0].ConsecutiveFailures != 5 {
		t.Errorf("expected 5 consecutive failures, got %d", *entries[0].ConsecutiveFailures)
	}
}
