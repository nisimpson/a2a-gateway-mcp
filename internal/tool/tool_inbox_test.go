package tool

import (
	"context"
	"testing"
	"time"

	"github.com/nisimpson/a2a-gateway-mcp/registry"
)

func newCheckInboxTool() (*CheckInboxTool, *mockInbox) {
	inbox := &mockInbox{}
	tool := &CheckInboxTool{Inbox: inbox}
	return tool, inbox
}

func TestCheckInbox_EmptyInbox(t *testing.T) {
	tool, _ := newCheckInboxTool()

	result, output, err := tool.Handle(context.Background(), nil, &CheckInboxInput{})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil result for success")
	}
	if output == nil {
		t.Fatal("expected non-nil output")
	}
	if len(output.Entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(output.Entries))
	}
}

func TestCheckInbox_MultipleEntries(t *testing.T) {
	tool, inbox := newCheckInboxTool()

	now := time.Now()
	inbox.Deposit(registry.InboxEntry{
		Alias:     "agent-a",
		TaskID:    "task-1",
		ContextID: "ctx-1",
		State:     "completed",
		Timestamp: now.Add(-2 * time.Minute),
	})
	inbox.Deposit(registry.InboxEntry{
		Alias:     "agent-b",
		TaskID:    "task-2",
		ContextID: "ctx-2",
		State:     "input-required",
		Timestamp: now.Add(-1 * time.Minute),
	})

	result, output, err := tool.Handle(context.Background(), nil, &CheckInboxInput{})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil result for success")
	}
	if output == nil {
		t.Fatal("expected non-nil output")
	}
	if len(output.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(output.Entries))
	}

	// Verify first entry
	if output.Entries[0].Alias != "agent-a" {
		t.Errorf("expected alias 'agent-a', got %q", output.Entries[0].Alias)
	}
	if output.Entries[0].TaskID != "task-1" {
		t.Errorf("expected task_id 'task-1', got %q", output.Entries[0].TaskID)
	}
	if output.Entries[0].ContextID != "ctx-1" {
		t.Errorf("expected context_id 'ctx-1', got %q", output.Entries[0].ContextID)
	}
	if output.Entries[0].State != "completed" {
		t.Errorf("expected state 'completed', got %q", output.Entries[0].State)
	}

	// Verify second entry
	if output.Entries[1].Alias != "agent-b" {
		t.Errorf("expected alias 'agent-b', got %q", output.Entries[1].Alias)
	}
	if output.Entries[1].State != "input-required" {
		t.Errorf("expected state 'input-required', got %q", output.Entries[1].State)
	}
}

func TestCheckInbox_AliasFilter(t *testing.T) {
	tool, inbox := newCheckInboxTool()

	now := time.Now()
	inbox.Deposit(registry.InboxEntry{
		Alias:     "agent-a",
		TaskID:    "task-1",
		State:     "completed",
		Timestamp: now.Add(-2 * time.Minute),
	})
	inbox.Deposit(registry.InboxEntry{
		Alias:     "agent-b",
		TaskID:    "task-2",
		State:     "completed",
		Timestamp: now.Add(-1 * time.Minute),
	})
	inbox.Deposit(registry.InboxEntry{
		Alias:     "agent-a",
		TaskID:    "task-3",
		State:     "failed",
		Timestamp: now,
	})

	result, output, err := tool.Handle(context.Background(), nil, &CheckInboxInput{Alias: "agent-a"})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil result for success")
	}
	if output == nil {
		t.Fatal("expected non-nil output")
	}
	if len(output.Entries) != 2 {
		t.Fatalf("expected 2 entries for agent-a, got %d", len(output.Entries))
	}
	for _, entry := range output.Entries {
		if entry.Alias != "agent-a" {
			t.Errorf("expected all entries to have alias 'agent-a', got %q", entry.Alias)
		}
	}
}

func TestCheckInbox_ChronologicalOrder(t *testing.T) {
	tool, inbox := newCheckInboxTool()

	t1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	// Deposit in chronological order
	inbox.Deposit(registry.InboxEntry{
		Alias:     "agent-a",
		TaskID:    "task-oldest",
		State:     "completed",
		Timestamp: t1,
	})
	inbox.Deposit(registry.InboxEntry{
		Alias:     "agent-a",
		TaskID:    "task-middle",
		State:     "completed",
		Timestamp: t2,
	})
	inbox.Deposit(registry.InboxEntry{
		Alias:     "agent-a",
		TaskID:    "task-newest",
		State:     "completed",
		Timestamp: t3,
	})

	result, output, err := tool.Handle(context.Background(), nil, &CheckInboxInput{})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil result for success")
	}
	if output == nil {
		t.Fatal("expected non-nil output")
	}
	if len(output.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(output.Entries))
	}

	// Verify chronological ordering (oldest first)
	expectedIDs := []string{"task-oldest", "task-middle", "task-newest"}
	for i, expected := range expectedIDs {
		if output.Entries[i].TaskID != expected {
			t.Errorf("entry %d: expected task_id %q, got %q", i, expected, output.Entries[i].TaskID)
		}
	}

	// Verify timestamps are in ascending order
	expectedTimestamps := []time.Time{t1, t2, t3}
	for i, expected := range expectedTimestamps {
		parsed, parseErr := time.Parse(time.RFC3339, output.Entries[i].Timestamp)
		if parseErr != nil {
			t.Fatalf("entry %d: failed to parse timestamp %q: %v", i, output.Entries[i].Timestamp, parseErr)
		}
		if !parsed.Equal(expected) {
			t.Errorf("entry %d: expected timestamp %v, got %v", i, expected, parsed)
		}
	}
}

func TestCheckInbox_AliasFilterNoMatch(t *testing.T) {
	tool, inbox := newCheckInboxTool()

	inbox.Deposit(registry.InboxEntry{
		Alias:     "agent-a",
		TaskID:    "task-1",
		State:     "completed",
		Timestamp: time.Now(),
	})

	result, output, err := tool.Handle(context.Background(), nil, &CheckInboxInput{Alias: "nonexistent"})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil result for success")
	}
	if output == nil {
		t.Fatal("expected non-nil output")
	}
	if len(output.Entries) != 0 {
		t.Errorf("expected empty entries for nonexistent alias, got %d", len(output.Entries))
	}
}

func TestCheckInbox_NonDestructive(t *testing.T) {
	tool, inbox := newCheckInboxTool()

	inbox.Deposit(registry.InboxEntry{
		Alias:     "agent-a",
		TaskID:    "task-1",
		State:     "completed",
		Timestamp: time.Now(),
	})

	// First peek
	_, output1, err := tool.Handle(context.Background(), nil, &CheckInboxInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(output1.Entries) != 1 {
		t.Fatalf("first peek: expected 1 entry, got %d", len(output1.Entries))
	}

	// Second peek should return same results (non-destructive)
	_, output2, err := tool.Handle(context.Background(), nil, &CheckInboxInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(output2.Entries) != 1 {
		t.Fatalf("second peek: expected 1 entry, got %d", len(output2.Entries))
	}
	if output2.Entries[0].TaskID != output1.Entries[0].TaskID {
		t.Error("second peek returned different entry than first peek")
	}
}
