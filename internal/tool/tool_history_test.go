package tool

import (
	"context"
	"testing"
	"time"

	"github.com/nisimpson/a2a-gateway-mcp/history"
	"github.com/nisimpson/a2a-gateway-mcp/registry"
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
	result, output, err := tool.Handle(context.Background(), nil, &GetHistoryInput{Agent: ""})
	if err == nil {
		t.Fatal("expected error for empty alias")
	}
	if result != nil || output != nil {
		t.Fatal("expected nil result and output for validation error")
	}
}

func TestGetHistory_AgentNotFound(t *testing.T) {
	tool, reg, _ := newGetHistoryTool()
	reg.LookupFn = func(alias string) *registry.RegisteredAgent { return nil }

	result, output, err := tool.Handle(context.Background(), nil, &GetHistoryInput{Agent: "ghost"})
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
	if result != nil || output != nil {
		t.Fatal("expected nil result and output for error")
	}
}

func TestGetHistory_Success(t *testing.T) {
	tool, reg, hb := newGetHistoryTool()

	reg.LookupFn = func(alias string) *registry.RegisteredAgent {
		return &registry.RegisteredAgent{Alias: "my-agent", URL: "http://example.com"}
	}

	now := time.Now()
	hb.ListFn = func(_ context.Context, alias string) ([]history.Entry, error) {
		return []history.Entry{
			{Timestamp: now.Format(time.RFC3339), SentMessage: "hello", Response: "world"},
			{Timestamp: now.Add(time.Second).Format(time.RFC3339), SentMessage: "foo", Response: "bar"},
		}, nil
	}

	result, output, err := tool.Handle(context.Background(), nil, &GetHistoryInput{Agent: "my-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil result for success")
	}
	if output == nil {
		t.Fatal("expected non-nil output for success")
	}
	if len(output.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(output.Entries))
	}
	if output.Entries[0].SentMessage != "hello" {
		t.Errorf("expected first entry sent_message='hello', got %q", output.Entries[0].SentMessage)
	}
}

func TestGetHistory_WithLimit(t *testing.T) {
	tool, reg, hb := newGetHistoryTool()

	reg.LookupFn = func(alias string) *registry.RegisteredAgent {
		return &registry.RegisteredAgent{Alias: "my-agent", URL: "http://example.com"}
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
	result, output, err := tool.Handle(context.Background(), nil, &GetHistoryInput{Agent: "my-agent", Limit: &limit})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil result for success")
	}
	if output == nil {
		t.Fatal("expected non-nil output for success")
	}
	if len(output.Entries) != 2 {
		t.Fatalf("expected 2 entries after limit, got %d", len(output.Entries))
	}
	// Should return the most recent 2.
	if output.Entries[0].SentMessage != "second" {
		t.Errorf("expected 'second', got %q", output.Entries[0].SentMessage)
	}
	if output.Entries[1].SentMessage != "third" {
		t.Errorf("expected 'third', got %q", output.Entries[1].SentMessage)
	}
}

func TestClearHistory_Success(t *testing.T) {
	tool, reg, hb := newClearHistoryTool()

	reg.LookupFn = func(alias string) *registry.RegisteredAgent {
		return &registry.RegisteredAgent{Alias: "my-agent", URL: "http://example.com"}
	}

	var clearCalled bool
	hb.ClearFn = func(_ context.Context, alias string) error {
		clearCalled = true
		return nil
	}

	result, output, err := tool.Handle(context.Background(), nil, &ClearHistoryInput{Agent: "my-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil result for success")
	}
	if output == nil {
		t.Fatal("expected non-nil output for success")
	}
	if !clearCalled {
		t.Error("expected Clear to be called")
	}
}

func TestClearHistory_AgentNotFound(t *testing.T) {
	tool, reg, _ := newClearHistoryTool()
	reg.LookupFn = func(alias string) *registry.RegisteredAgent { return nil }

	result, output, err := tool.Handle(context.Background(), nil, &ClearHistoryInput{Agent: "ghost"})
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
	if result != nil || output != nil {
		t.Fatal("expected nil result and output for error")
	}
}
