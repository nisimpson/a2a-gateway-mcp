package gateway

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Unit tests for handleGetHistory — validates Requirements 2.4 and 2.5.

func TestHandleGetHistory_UnregisteredAlias(t *testing.T) {
	srv := NewServer()

	input := GetHistoryInput{
		Agent: "nonexistent-agent",
	}

	result, _, err := srv.handleGetHistory(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for unregistered alias")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text != "agent not found in registry" {
		t.Errorf("expected %q, got %q", "agent not found in registry", textContent.Text)
	}
}

func TestHandleGetHistory_NoHistory(t *testing.T) {
	srv := NewServer()

	// Register an agent but don't record any history.
	srv.registry.Connect("test-agent", "http://localhost:9999", nil, "")

	input := GetHistoryInput{
		Agent: "test-agent",
	}

	result, _, err := srv.handleGetHistory(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text != "[]" {
		t.Errorf("expected empty JSON array %q, got %q", "[]", textContent.Text)
	}
}

func TestHandleGetHistory_LimitParameter(t *testing.T) {
	srv := NewServer()

	// Register an agent.
	srv.registry.Connect("history-agent", "http://localhost:9999", nil, "")

	// Append several entries directly to the backend.
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		entry := HistoryEntry{
			Timestamp: time.Date(2025, 1, 1, 0, i, 0, 0, time.UTC),
			SentMsg:   "message " + string(rune('A'+i)),
			Response:  "response " + string(rune('A'+i)),
		}
		if err := srv.historyBackend.Append(ctx, "history-agent", entry); err != nil {
			t.Fatalf("failed to append entry %d: %v", i, err)
		}
	}

	// Request only the last 3 entries.
	limit := 3
	input := GetHistoryInput{
		Agent: "history-agent",
		Limit: &limit,
	}

	result, _, err := srv.handleGetHistory(ctx, nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}

	var entries []HistoryEntry
	if err := json.Unmarshal([]byte(textContent.Text), &entries); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Should be the most recent 3 entries (indices 2, 3, 4 of the original 5).
	expectedMessages := []string{"message C", "message D", "message E"}
	for i, entry := range entries {
		if entry.SentMsg != expectedMessages[i] {
			t.Errorf("entry %d: expected sent_message %q, got %q", i, expectedMessages[i], entry.SentMsg)
		}
	}
}
