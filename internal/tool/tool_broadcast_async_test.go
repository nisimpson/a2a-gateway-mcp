package tool

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/nisimpson/a2a-gateway-mcp/history"
	"github.com/nisimpson/a2a-gateway-mcp/registry"
)

// safeHistoryRecorder is a thread-safe version of mockHistoryRecorder for async tests.
type safeHistoryRecorder struct {
	mu      sync.Mutex
	records []history.RecordInput
}

func (m *safeHistoryRecorder) Record(_ context.Context, input history.RecordInput) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, input)
}

func newBroadcastToolWithInbox(reg *mockRegistry, clientResolver *mockClientResolver, inbox *mockInbox) *BroadcastMessageTool {
	return &BroadcastMessageTool{
		AgentRegistry:      reg,
		A2AClientResolver:  clientResolver,
		CallerCardInjector: &mockCallerCardInjector{},
		HealthTracker:      &mockHealthTracker{},
		HistoryRecorder:    &safeHistoryRecorder{},
		RateLimiter:        &mockRateLimiter{},
		Inbox:              inbox,
	}
}

func TestBroadcastAsync_ReturnsImmediately(t *testing.T) {
	// Set up a slow agent that takes a while to respond.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		req, _ := readJSONRPCRequest(r)
		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("async reply"))
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
	inbox := &mockInbox{}

	b := newBroadcastToolWithInbox(reg, clientResolver, inbox)

	start := time.Now()
	result, out, err := b.Handle(context.Background(), nil, &BroadcastMessageInput{
		Aliases: []string{"agent1", "agent2"},
		Message: "hello async",
		Async:   boolPtr(true),
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatal("expected nil result for async broadcast (structured output used)")
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 entries in output map, got %d", len(out))
	}

	// Should return much faster than the agent's 500ms delay.
	if elapsed > 200*time.Millisecond {
		t.Fatalf("async broadcast took too long: %v (expected <200ms)", elapsed)
	}

	// Verify each alias has async output with "dispatched" status.
	for _, alias := range []string{"agent1", "agent2"} {
		entry, ok := out[alias]
		if !ok {
			t.Fatalf("expected entry for %q in output", alias)
		}
		if entry.Async == nil {
			t.Fatalf("expected Async field set for %q", alias)
		}
		if entry.Async.Status != "dispatched" {
			t.Fatalf("expected status 'dispatched' for %q, got %q", alias, entry.Async.Status)
		}
	}
}

func TestBroadcastAsync_InboxEntriesDeposited(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("background reply"))
		msg.ContextID = "ctx-123"
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
	inbox := &mockInbox{}

	b := newBroadcastToolWithInbox(reg, clientResolver, inbox)

	_, _, err := b.Handle(context.Background(), nil, &BroadcastMessageInput{
		Aliases: []string{"agent1"},
		Message: "hello",
		Async:   boolPtr(true),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for the background goroutine to complete and deposit into inbox.
	deadline := time.Now().Add(3 * time.Second)
	for {
		inbox.mu.Lock()
		count := len(inbox.entries)
		inbox.mu.Unlock()
		if count > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for inbox deposit")
		}
		time.Sleep(50 * time.Millisecond)
	}

	inbox.mu.Lock()
	entries := make([]registry.InboxEntry, len(inbox.entries))
	copy(entries, inbox.entries)
	inbox.mu.Unlock()

	if len(entries) != 1 {
		t.Fatalf("expected 1 inbox entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Alias != "agent1" {
		t.Fatalf("expected alias 'agent1', got %q", entry.Alias)
	}
	if entry.State != "completed" {
		t.Fatalf("expected state 'completed', got %q", entry.State)
	}
	if entry.Message == nil {
		t.Fatal("expected message in inbox entry")
	}
	if entry.ContextID != "ctx-123" {
		t.Fatalf("expected context_id 'ctx-123', got %q", entry.ContextID)
	}
}

func TestBroadcastAsync_PartialFailures(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("reply"))
		writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
	}))
	defer agent.Close()

	reg := &mockRegistry{
		LookupFn: func(alias string) *registry.RegisteredAgent {
			if alias == "unknown" {
				return nil
			}
			return &registry.RegisteredAgent{Alias: alias, URL: agent.URL}
		},
	}
	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}
	rateLimiter := &mockRateLimiter{
		CheckRateLimitFn: func(alias string) error {
			if alias == "limited" {
				return fmt.Errorf("agent %q has exceeded its rate limit", alias)
			}
			return nil
		},
	}
	inbox := &mockInbox{}

	b := newBroadcastToolWithInbox(reg, clientResolver, inbox)
	b.RateLimiter = rateLimiter

	result, out, err := b.Handle(context.Background(), nil, &BroadcastMessageInput{
		Aliases: []string{"valid", "unknown", "limited"},
		Message: "hello",
		Async:   boolPtr(true),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatal("expected nil result for async broadcast")
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 entries in output map, got %d", len(out))
	}

	// "valid" should be dispatched.
	if out["valid"].Async == nil || out["valid"].Async.Status != "dispatched" {
		t.Fatalf("expected 'dispatched' for 'valid', got %v", out["valid"])
	}

	// "unknown" should be an error (not registered).
	if out["unknown"].Async == nil || out["unknown"].Async.Status != "error" {
		t.Fatalf("expected 'error' for 'unknown', got %v", out["unknown"])
	}
	if !contains(out["unknown"].Async.Error, "not registered") {
		t.Fatalf("expected 'not registered' in error, got %q", out["unknown"].Async.Error)
	}

	// "limited" should be an error (rate limited).
	if out["limited"].Async == nil || out["limited"].Async.Status != "error" {
		t.Fatalf("expected 'error' for 'limited', got %v", out["limited"])
	}
	if !contains(out["limited"].Async.Error, "rate limited") {
		t.Fatalf("expected 'rate limited' in error, got %q", out["limited"].Async.Error)
	}
}

func TestBroadcastAsync_SyncPathUnchanged(t *testing.T) {
	// Verify that when async is false/nil, the existing sync path works as before.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("sync reply"))
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
	inbox := &mockInbox{}

	b := newBroadcastToolWithInbox(reg, clientResolver, inbox)

	// Test with async: false.
	result, out, err := b.Handle(context.Background(), nil, &BroadcastMessageInput{
		Aliases: []string{"agent1"},
		Message: "hello sync",
		Async:   boolPtr(false),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatal("expected nil result for sync success")
	}
	if out == nil {
		t.Fatal("expected structured output for sync broadcast")
	}
	if _, ok := out["agent1"]; !ok {
		t.Fatal("expected agent1 in sync output")
	}

	// Verify no inbox deposits for sync path.
	inbox.mu.Lock()
	count := len(inbox.entries)
	inbox.mu.Unlock()
	if count != 0 {
		t.Fatalf("expected 0 inbox deposits for sync path, got %d", count)
	}

	// Test with async: nil (omitted).
	result2, out2, err2 := b.Handle(context.Background(), nil, &BroadcastMessageInput{
		Aliases: []string{"agent1"},
		Message: "hello nil",
	})
	if err2 != nil {
		t.Fatalf("unexpected error: %v", err2)
	}
	if result2 != nil {
		t.Fatal("expected nil result for sync success (nil async)")
	}
	if out2 == nil {
		t.Fatal("expected structured output for sync broadcast (nil async)")
	}

	// Still no inbox deposits.
	inbox.mu.Lock()
	count = len(inbox.entries)
	inbox.mu.Unlock()
	if count != 0 {
		t.Fatalf("expected 0 inbox deposits for sync path (nil async), got %d", count)
	}
}
