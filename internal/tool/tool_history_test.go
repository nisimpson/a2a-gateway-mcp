package tool

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nisimpson/a2a-gateway-mcp/internal/history"
)

func newGetHistoryTool() (*GetHistoryTool, *mockRegistry, *mockHistoryBackend) {
	reg := &mockRegistry{}
	hb := &mockHistoryBackend{}
	tool := &GetHistoryTool{
		AgentRegistry:  reg,
		HistoryBackend: hb,
	}
	return tool, reg, hb
}

func newClearHistoryTool() (*ClearHistoryTool, *mockRegistry, *mockHistoryBackend) {
	reg := &mockRegistry{}
	hb := &mockHistoryBackend{}
	tool := &ClearHistoryTool{
		AgentRegistry:  reg,
		HistoryBackend: hb,
	}
	return tool, reg, hb
}

func TestGetHistory_EmptyAlias(t *testing.T) {
	tool, _, _ := newGetHistoryTool()
	result, _, err := tool.Handle(context.Background(), nil, &GetHistoryInput{Agent: ""})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result for empty alias")
	}
	assertTextContains(t, result, "agent alias is required")
}

func TestGetHistory_AgentNotFound(t *testing.T) {
	tool, reg, _ := newGetHistoryTool()
	reg.LookupFn = func(alias string) *AgentEntry { return nil }

	result, _, err := tool.Handle(context.Background(), nil, &GetHistoryInput{Agent: "ghost"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result for unknown agent")
	}
	assertTextContains(t, result, "not found")
}

func TestGetHistory_Success(t *testing.T) {
	tool, reg, hb := newGetHistoryTool()

	reg.LookupFn = func(alias string) *AgentEntry {
		return &AgentEntry{Alias: "my-agent", URL: "http://example.com"}
	}

	now := time.Now()
	hb.ListFn = func(_ context.Context, alias string) ([]history.Entry, error) {
		return []history.Entry{
			{Timestamp: now.Format(time.RFC3339), SentMessage: "hello", Response: "world"},
			{Timestamp: now.Add(time.Second).Format(time.RFC3339), SentMessage: "foo", Response: "bar"},
		}, nil
	}

	result, _, err := tool.Handle(context.Background(), nil, &GetHistoryInput{Agent: "my-agent"})
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

	var entries []history.Entry
	if err := json.Unmarshal([]byte(tc.Text), &entries); err != nil {
		t.Fatalf("failed to unmarshal history: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].SentMessage != "hello" {
		t.Errorf("expected first entry sent_message='hello', got %q", entries[0].SentMessage)
	}
}

func TestGetHistory_WithLimit(t *testing.T) {
	tool, reg, hb := newGetHistoryTool()

	reg.LookupFn = func(alias string) *AgentEntry {
		return &AgentEntry{Alias: "my-agent", URL: "http://example.com"}
	}

	now := time.Now()
	hb.ListFn = func(_ context.Context, alias string) ([]history.Entry, error) {
		return []history.Entry{
			{Timestamp: now.Format(time.RFC3339), SentMessage: "first", Response: "r1"},
			{Timestamp: now.Add(time.Second).Format(time.RFC3339), SentMessage: "second", Response: "r2"},
			{Timestamp: now.Add(2 * time.Second).Format(time.RFC3339), SentMessage: "third", Response: "r3"},
		}, nil
	}

	limit := 2
	result, _, err := tool.Handle(context.Background(), nil, &GetHistoryInput{Agent: "my-agent", Limit: &limit})
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

	var entries []history.Entry
	if err := json.Unmarshal([]byte(tc.Text), &entries); err != nil {
		t.Fatalf("failed to unmarshal history: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries after limit, got %d", len(entries))
	}
	// Should return the most recent 2.
	if entries[0].SentMessage != "second" {
		t.Errorf("expected 'second', got %q", entries[0].SentMessage)
	}
	if entries[1].SentMessage != "third" {
		t.Errorf("expected 'third', got %q", entries[1].SentMessage)
	}
}

func TestClearHistory_Success(t *testing.T) {
	tool, reg, hb := newClearHistoryTool()

	reg.LookupFn = func(alias string) *AgentEntry {
		return &AgentEntry{Alias: "my-agent", URL: "http://example.com"}
	}

	var clearCalled bool
	hb.ClearFn = func(_ context.Context, alias string) error {
		clearCalled = true
		return nil
	}

	result, _, err := tool.Handle(context.Background(), nil, &ClearHistoryInput{Agent: "my-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}
	if !clearCalled {
		t.Error("expected Clear to be called")
	}
	assertTextContains(t, result, "Cleared history")
}

func TestClearHistory_AgentNotFound(t *testing.T) {
	tool, reg, _ := newClearHistoryTool()
	reg.LookupFn = func(alias string) *AgentEntry { return nil }

	result, _, err := tool.Handle(context.Background(), nil, &ClearHistoryInput{Agent: "ghost"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result for unknown agent")
	}
	assertTextContains(t, result, "not found")
}
