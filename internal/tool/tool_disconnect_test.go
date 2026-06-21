package tool

import (
	"context"
	"testing"
)

func newDisconnectTool() (*DisconnectAgentTool, *mockRegistry, *mockClientResolver, *mockRateLimiter, *mockHealthTracker, *mockContextStore) {
	reg := &mockRegistry{}
	resolver := &mockClientResolver{}
	rl := &mockRateLimiter{}
	ht := &mockHealthTracker{}
	cs := newMockContextStore()
	tool := &DisconnectAgentTool{
		AgentRegistry:     reg,
		A2AClientResolver: resolver,
		ContextStore:      cs,
		RateLimiter:       rl,
		HealthTracker:     ht,
		HistoryBackend:    nil,
	}
	return tool, reg, resolver, rl, ht, cs
}

func TestDisconnect_InvalidAlias(t *testing.T) {
	tool, _, _, _, _, _ := newDisconnectTool()
	result, _, err := tool.Handle(context.Background(), nil, &DisconnectAgentInput{Alias: ""})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result for empty alias")
	}
	assertTextContains(t, result, "alias is required")
}

func TestDisconnect_NotFound(t *testing.T) {
	tool, reg, _, _, _, _ := newDisconnectTool()
	reg.DisconnectFn = func(alias string) *AgentEntry {
		return nil
	}
	result, _, err := tool.Handle(context.Background(), nil, &DisconnectAgentInput{Alias: "ghost"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result for unknown alias")
	}
	assertTextContains(t, result, "not found")
}

func TestDisconnect_Success(t *testing.T) {
	tool, reg, resolver, rl, ht, cs := newDisconnectTool()

	reg.DisconnectFn = func(alias string) *AgentEntry {
		return &AgentEntry{Alias: "my-agent", URL: "http://example.com"}
	}

	var evictCalled bool
	resolver.EvictFn = func(url string) {
		evictCalled = true
	}

	var removeCalled bool
	rl.RemoveFn = func(alias string) {
		removeCalled = true
	}

	var resetCalled bool
	ht.ResetFn = func(alias string) {
		resetCalled = true
	}

	// Seed context store.
	cs.Set("my-agent", "some-ctx")

	var historyDeleteCalled bool
	hb := &mockHistoryBackend{
		DeleteFn: func(_ context.Context, alias string) error {
			historyDeleteCalled = true
			return nil
		},
	}
	tool.HistoryBackend = hb

	result, _, err := tool.Handle(context.Background(), nil, &DisconnectAgentInput{Alias: "my-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}
	if !evictCalled {
		t.Error("expected Evict to be called")
	}
	if !removeCalled {
		t.Error("expected RateLimiter.Remove to be called")
	}
	if !resetCalled {
		t.Error("expected HealthTracker.Reset to be called")
	}
	if cs.Get("my-agent") != "" {
		t.Error("expected context store to be cleared")
	}
	if !historyDeleteCalled {
		t.Error("expected HistoryBackend.Delete to be called")
	}
	assertTextContains(t, result, "Disconnected agent")
}

func TestDisconnect_NoHistory(t *testing.T) {
	tool, reg, _, _, _, _ := newDisconnectTool()

	reg.DisconnectFn = func(alias string) *AgentEntry {
		return &AgentEntry{Alias: "my-agent", URL: "http://example.com"}
	}

	// HistoryBackend is nil — should not panic.
	tool.HistoryBackend = nil

	result, _, err := tool.Handle(context.Background(), nil, &DisconnectAgentInput{Alias: "my-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}
	assertTextContains(t, result, "Disconnected agent")
}
