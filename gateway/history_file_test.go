package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// Feature: task-history, Property 8: File backend round-trip preserves entries
// **Validates: Requirements 7.4**

func TestPropertyFileBackendRoundTripPreservesEntries(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for random strings (1-200 chars)
	stringGen := gen.RegexMatch(`[a-zA-Z0-9 _\-\.]{1,200}`)

	// Generator for UUIDs (context ID, task ID)
	uuidGen := gen.RegexMatch(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

	// Generator for boolean (isError)
	boolGen := gen.Bool()

	// Generator for timestamps (random seconds offset from a base time)
	timestampOffsetGen := gen.IntRange(0, 1000000)

	properties.Property("Appending a HistoryEntry to FileBackend and calling List returns an entry equal to the appended entry after JSON round-trip", prop.ForAll(
		func(sentMsg string, response string, contextID string, taskID string, isError bool, tsOffset int) bool {
			// Use a temp directory per test iteration
			dir := t.TempDir()

			backend, err := NewFileBackend(dir, 50)
			if err != nil {
				t.Logf("NewFileBackend returned error: %v", err)
				return false
			}

			ctx := context.Background()
			alias := "test-agent"

			// Build entry with a specific timestamp (truncated to second precision
			// since JSON marshaling of time.Time preserves nanoseconds but we want
			// to verify the round-trip faithfully)
			baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
			timestamp := baseTime.Add(time.Duration(tsOffset) * time.Second)

			entry := HistoryEntry{
				Timestamp: timestamp,
				SentMsg:   sentMsg,
				Response:  response,
				ContextID: contextID,
				TaskID:    taskID,
				IsError:   isError,
			}

			// Append the entry
			if err := backend.Append(ctx, alias, entry); err != nil {
				t.Logf("Append returned error: %v", err)
				return false
			}

			// List entries back
			entries, err := backend.List(ctx, alias)
			if err != nil {
				t.Logf("List returned error: %v", err)
				return false
			}

			if len(entries) != 1 {
				t.Logf("expected 1 entry, got %d", len(entries))
				return false
			}

			got := entries[0]

			// Field-by-field comparison after JSON round-trip normalization

			// Verify timestamp is preserved
			if !got.Timestamp.Equal(entry.Timestamp) {
				t.Logf("Timestamp mismatch: got %v, want %v", got.Timestamp, entry.Timestamp)
				return false
			}

			// Verify sent message is preserved
			if got.SentMsg != entry.SentMsg {
				t.Logf("SentMsg mismatch: got %q, want %q", got.SentMsg, entry.SentMsg)
				return false
			}

			// Verify response is preserved
			if got.Response != entry.Response {
				t.Logf("Response mismatch: got %q, want %q", got.Response, entry.Response)
				return false
			}

			// Verify context ID is preserved
			if got.ContextID != entry.ContextID {
				t.Logf("ContextID mismatch: got %q, want %q", got.ContextID, entry.ContextID)
				return false
			}

			// Verify task ID is preserved
			if got.TaskID != entry.TaskID {
				t.Logf("TaskID mismatch: got %q, want %q", got.TaskID, entry.TaskID)
				return false
			}

			// Verify error flag is preserved
			if got.IsError != entry.IsError {
				t.Logf("IsError mismatch: got %v, want %v", got.IsError, entry.IsError)
				return false
			}

			return true
		},
		stringGen,
		stringGen,
		uuidGen,
		uuidGen,
		boolGen,
		timestampOffsetGen,
	))

	properties.TestingRun(t)
}
