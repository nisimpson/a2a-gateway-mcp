package tool

import (
	"context"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/nisimpson/a2a-gateway-mcp/registry"
)

func newReadInboxTool() (*ReadInboxTool, *registry.MemoryInbox) {
	inbox := registry.NewMemoryInbox(30 * time.Minute)
	tool := &ReadInboxTool{Inbox: inbox}
	return tool, inbox
}

func TestReadInbox_FIFOOrdering(t *testing.T) {
	tool, inbox := newReadInboxTool()

	// Deposit entries in order
	for i, id := range []string{"task-1", "task-2", "task-3"} {
		inbox.Deposit(registry.InboxEntry{
			Alias:     "agent-a",
			TaskID:    id,
			State:     "completed",
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			Task:      &a2a.Task{ID: a2a.TaskID(id)},
		})
	}

	_, output, err := tool.Handle(context.Background(), nil, &ReadInboxInput{Alias: "agent-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(output.Messages))
	}

	// Verify FIFO order (oldest first)
	expectedIDs := []a2a.TaskID{"task-1", "task-2", "task-3"}
	for i, expected := range expectedIDs {
		if output.Messages[i].Task == nil {
			t.Fatalf("message %d: expected non-nil Task", i)
		}
		if output.Messages[i].Task.ID != expected {
			t.Errorf("message %d: expected task ID %q, got %q", i, expected, output.Messages[i].Task.ID)
		}
	}
}

func TestReadInbox_LengthLimit(t *testing.T) {
	tool, inbox := newReadInboxTool()

	// Deposit 5 entries
	for i := range 5 {
		inbox.Deposit(registry.InboxEntry{
			Alias:     "agent-a",
			TaskID:    "task-" + string(rune('1'+i)),
			State:     "completed",
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			Task:      &a2a.Task{ID: a2a.TaskID("task-" + string(rune('1'+i)))},
		})
	}

	length := 2
	_, output, err := tool.Handle(context.Background(), nil, &ReadInboxInput{
		Alias:  "agent-a",
		Length: &length,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Messages) != 2 {
		t.Fatalf("expected 2 messages with length=2, got %d", len(output.Messages))
	}

	// Verify we got the first 2 (FIFO)
	if output.Messages[0].Task.ID != "task-1" {
		t.Errorf("first message: expected task-1, got %q", output.Messages[0].Task.ID)
	}
	if output.Messages[1].Task.ID != "task-2" {
		t.Errorf("second message: expected task-2, got %q", output.Messages[1].Task.ID)
	}

	// Verify remaining entries still in inbox (3 left)
	remaining := inbox.Peek(registry.InboxPeekFilter{Alias: "agent-a"})
	if len(remaining) != 3 {
		t.Errorf("expected 3 remaining entries after length-limited read, got %d", len(remaining))
	}
}

func TestReadInbox_LatestMode(t *testing.T) {
	tool, inbox := newReadInboxTool()

	// Deposit 4 entries
	for i := range 4 {
		inbox.Deposit(registry.InboxEntry{
			Alias:     "agent-a",
			TaskID:    "task-" + string(rune('1'+i)),
			State:     "completed",
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			Task:      &a2a.Task{ID: a2a.TaskID("task-" + string(rune('1'+i)))},
		})
	}

	_, output, err := tool.Handle(context.Background(), nil, &ReadInboxInput{
		Alias:  "agent-a",
		Latest: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Latest mode: returns only the most recent entry
	if len(output.Messages) != 1 {
		t.Fatalf("expected 1 message with latest=true, got %d", len(output.Messages))
	}
	if output.Messages[0].Task.ID != "task-4" {
		t.Errorf("expected most recent task (task-4), got %q", output.Messages[0].Task.ID)
	}

	// All entries for the alias should be removed
	remaining := inbox.Peek(registry.InboxPeekFilter{Alias: "agent-a"})
	if len(remaining) != 0 {
		t.Errorf("expected 0 remaining entries after latest read, got %d", len(remaining))
	}
}

func TestReadInbox_EmptyAlias(t *testing.T) {
	tool, _ := newReadInboxTool()

	_, _, err := tool.Handle(context.Background(), nil, &ReadInboxInput{Alias: ""})
	if err == nil {
		t.Fatal("expected error for empty alias")
	}
	if err.Error() != "alias is required" {
		t.Errorf("expected 'alias is required' error, got %q", err.Error())
	}
}

func TestReadInbox_EntriesRemovedAfterRead(t *testing.T) {
	tool, inbox := newReadInboxTool()

	inbox.Deposit(registry.InboxEntry{
		Alias:     "agent-a",
		TaskID:    "task-1",
		State:     "completed",
		Timestamp: time.Now(),
		Task:      &a2a.Task{ID: "task-1"},
	})
	inbox.Deposit(registry.InboxEntry{
		Alias:     "agent-a",
		TaskID:    "task-2",
		State:     "completed",
		Timestamp: time.Now().Add(time.Second),
		Task:      &a2a.Task{ID: "task-2"},
	})

	// Read all entries
	_, output, err := tool.Handle(context.Background(), nil, &ReadInboxInput{Alias: "agent-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(output.Messages))
	}

	// Peek should show nothing remaining
	remaining := inbox.Peek(registry.InboxPeekFilter{Alias: "agent-a"})
	if len(remaining) != 0 {
		t.Errorf("expected 0 entries after read, got %d", len(remaining))
	}

	// Second read should return empty
	_, output2, err := tool.Handle(context.Background(), nil, &ReadInboxInput{Alias: "agent-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(output2.Messages) != 0 {
		t.Errorf("expected 0 messages on second read, got %d", len(output2.Messages))
	}
}

func TestReadInbox_EmptyInbox(t *testing.T) {
	tool, _ := newReadInboxTool()

	_, output, err := tool.Handle(context.Background(), nil, &ReadInboxInput{Alias: "agent-a"})
	if err != nil {
		t.Fatal(err)
	}
	if output == nil {
		t.Fatal("expected non-nil output")
	}
	if len(output.Messages) != 0 {
		t.Errorf("expected empty messages array, got %d", len(output.Messages))
	}
}

func TestReadInbox_MessagePayload(t *testing.T) {
	tool, inbox := newReadInboxTool()

	// Deposit entry with a Message (not a Task)
	msg := &a2a.Message{
		Role: a2a.MessageRoleAgent,
		Parts: a2a.ContentParts{
			a2a.NewTextPart("hello world"),
		},
	}
	inbox.Deposit(registry.InboxEntry{
		Alias:     "agent-a",
		State:     "completed",
		Timestamp: time.Now(),
		Message:   msg,
	})

	_, output, err := tool.Handle(context.Background(), nil, &ReadInboxInput{Alias: "agent-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(output.Messages))
	}
	if output.Messages[0].Message == nil {
		t.Fatal("expected non-nil Message field")
	}
	if output.Messages[0].Task != nil {
		t.Error("expected nil Task field for message-type entry")
	}
}

func TestReadInbox_LengthGreaterThanAvailable(t *testing.T) {
	tool, inbox := newReadInboxTool()

	// Deposit only 2 entries
	inbox.Deposit(registry.InboxEntry{
		Alias:     "agent-a",
		TaskID:    "task-1",
		State:     "completed",
		Timestamp: time.Now(),
		Task:      &a2a.Task{ID: "task-1"},
	})
	inbox.Deposit(registry.InboxEntry{
		Alias:     "agent-a",
		TaskID:    "task-2",
		State:     "completed",
		Timestamp: time.Now().Add(time.Second),
		Task:      &a2a.Task{ID: "task-2"},
	})

	// Request more than available
	length := 10
	_, output, err := tool.Handle(context.Background(), nil, &ReadInboxInput{
		Alias:  "agent-a",
		Length: &length,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Should return all available without error (AINB-4.4)
	if len(output.Messages) != 2 {
		t.Fatalf("expected 2 messages (all available), got %d", len(output.Messages))
	}
}
