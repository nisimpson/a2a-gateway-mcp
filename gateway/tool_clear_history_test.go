package gateway

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Unit tests for handleClearHistory — validates Requirements 4.3 and 4.4.

func TestHandleClearHistory_UnregisteredAlias(t *testing.T) {
	srv := NewServer()

	input := ClearHistoryInput{
		Agent: "nonexistent-agent",
	}

	result, _, err := srv.handleClearHistory(context.Background(), nil, input)
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

func TestHandleClearHistory_Success(t *testing.T) {
	srv := NewServer()

	// Register an agent and append some history entries.
	srv.registry.Connect("clear-agent", "http://localhost:9999", nil, "")

	ctx := context.Background()
	for i := range 3 {
		entry := HistoryEntry{
			Timestamp: time.Date(2025, 1, 1, 0, i, 0, 0, time.UTC),
			SentMsg:   "message",
			Response:  "response",
		}
		if err := srv.historyBackend.Append(ctx, "clear-agent", entry); err != nil {
			t.Fatalf("failed to append entry %d: %v", i, err)
		}
	}

	// Call clear_history.
	input := ClearHistoryInput{
		Agent: "clear-agent",
	}

	result, _, err := srv.handleClearHistory(ctx, nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Verify the success confirmation text contains the agent alias.
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	expectedText := `Cleared history for agent "clear-agent"`
	if textContent.Text != expectedText {
		t.Errorf("expected %q, got %q", expectedText, textContent.Text)
	}

	// Verify that get_history now returns an empty list.
	getInput := GetHistoryInput{
		Agent: "clear-agent",
	}
	getResult, _, err := srv.handleGetHistory(ctx, nil, getInput)
	if err != nil {
		t.Fatalf("unexpected error from get_history: %v", err)
	}
	if getResult.IsError {
		t.Fatalf("expected success from get_history, got error: %v", getResult.Content)
	}

	getTextContent, ok := getResult.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent from get_history")
	}

	var entries []HistoryEntry
	if err := json.Unmarshal([]byte(getTextContent.Text), &entries); err != nil {
		t.Fatalf("failed to parse get_history response: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty history after clear, got %d entries", len(entries))
	}
}
