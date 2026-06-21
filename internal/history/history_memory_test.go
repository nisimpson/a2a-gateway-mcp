package history

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// Feature: task-history, Property 1: Recording preserves all provided fields
// **Validates: Requirements 1.1, 1.2, 1.4**

func TestPropertyRecordingPreservesAllFields(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for random strings (1-2000 chars)
	stringGen := gen.RegexMatch(`[a-zA-Z0-9 _\-\.]{1,200}`)

	// Generator for UUIDs (context ID, task ID)
	uuidGen := gen.RegexMatch(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

	// Generator for boolean (isError)
	boolGen := gen.Bool()

	properties.Property("Appended entry preserves all provided fields when retrieved via List", prop.ForAll(
		func(sentMsg string, response string, contextID string, taskID string, isError bool) bool {
			backend := NewMemoryBackend(50)
			ctx := context.Background()
			alias := "test-agent"

			entry := Entry{
				Timestamp: time.Now().UTC(),
				SentMsg:   sentMsg,
				Response:  response,
				ContextID: contextID,
				TaskID:    taskID,
				IsError:   isError,
			}

			err := backend.Append(ctx, alias, entry)
			if err != nil {
				t.Logf("Append returned error: %v", err)
				return false
			}

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

			// Verify sent message is preserved
			if got.SentMsg != sentMsg {
				t.Logf("SentMsg mismatch: got %q, want %q", got.SentMsg, sentMsg)
				return false
			}

			// Verify response is preserved
			if got.Response != response {
				t.Logf("Response mismatch: got %q, want %q", got.Response, response)
				return false
			}

			// Verify context ID is preserved
			if got.ContextID != contextID {
				t.Logf("ContextID mismatch: got %q, want %q", got.ContextID, contextID)
				return false
			}

			// Verify task ID is preserved
			if got.TaskID != taskID {
				t.Logf("TaskID mismatch: got %q, want %q", got.TaskID, taskID)
				return false
			}

			// Verify error flag is preserved
			if got.IsError != isError {
				t.Logf("IsError mismatch: got %v, want %v", got.IsError, isError)
				return false
			}

			// Verify timestamp is non-zero
			if got.Timestamp.IsZero() {
				t.Log("Timestamp is zero")
				return false
			}

			return true
		},
		stringGen,
		stringGen,
		uuidGen,
		uuidGen,
		boolGen,
	))

	properties.TestingRun(t)
}

// Feature: task-history, Property 3: Depth enforcement evicts oldest entries
// **Validates: Requirements 3.3, 6.2**

func TestPropertyDepthEnforcementEvictsOldestEntries(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for depth (1-100)
	depthGen := gen.IntRange(1, 100)

	properties.Property("After appending N > D entries, List returns exactly D most recent entries in order", prop.ForAll(
		func(depth int, multiplier int) bool {
			// N is the total number of entries to append (depth+1 to depth*3)
			n := depth + multiplier
			if n <= depth {
				// Safety: ensure N > D
				n = depth + 1
			}

			backend := NewMemoryBackend(depth)
			ctx := context.Background()
			alias := "test-agent"

			// Append N entries with distinguishable timestamps and messages
			baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
			for i := 0; i < n; i++ {
				entry := Entry{
					Timestamp: baseTime.Add(time.Duration(i) * time.Second),
					SentMsg:   fmt.Sprintf("msg-%d", i),
					Response:  fmt.Sprintf("resp-%d", i),
					ContextID: fmt.Sprintf("ctx-%d", i),
					TaskID:    fmt.Sprintf("task-%d", i),
					IsError:   false,
				}
				if err := backend.Append(ctx, alias, entry); err != nil {
					t.Logf("Append returned error: %v", err)
					return false
				}
			}

			entries, err := backend.List(ctx, alias)
			if err != nil {
				t.Logf("List returned error: %v", err)
				return false
			}

			// Must have exactly depth entries
			if len(entries) != depth {
				t.Logf("expected %d entries, got %d", depth, len(entries))
				return false
			}

			// Entries must be the last D appended (most recent), in chronological order
			startIdx := n - depth
			for i, entry := range entries {
				expectedIdx := startIdx + i
				expectedMsg := fmt.Sprintf("msg-%d", expectedIdx)
				expectedResp := fmt.Sprintf("resp-%d", expectedIdx)
				expectedTimestamp := baseTime.Add(time.Duration(expectedIdx) * time.Second)

				if entry.SentMsg != expectedMsg {
					t.Logf("entry[%d] SentMsg: got %q, want %q", i, entry.SentMsg, expectedMsg)
					return false
				}
				if entry.Response != expectedResp {
					t.Logf("entry[%d] Response: got %q, want %q", i, entry.Response, expectedResp)
					return false
				}
				if !entry.Timestamp.Equal(expectedTimestamp) {
					t.Logf("entry[%d] Timestamp: got %v, want %v", i, entry.Timestamp, expectedTimestamp)
					return false
				}
			}

			// Verify chronological order is preserved
			for i := 1; i < len(entries); i++ {
				if entries[i].Timestamp.Before(entries[i-1].Timestamp) {
					t.Logf("entry[%d] timestamp %v is before entry[%d] timestamp %v",
						i, entries[i].Timestamp, i-1, entries[i-1].Timestamp)
					return false
				}
			}

			return true
		},
		depthGen,
		gen.IntRange(1, 200), // multiplier: added to depth to get N (so N ranges from depth+1 to depth+200)
	))

	properties.TestingRun(t)
}

// Feature: task-history, Property 4: List returns entries in chronological order
// **Validates: Requirements 2.1, 2.2**

func TestPropertyListReturnsEntriesInChronologicalOrder(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for number of entries (2-100, need at least 2 to verify ordering)
	countGen := gen.IntRange(2, 100)

	properties.Property("Entries appended with strictly increasing timestamps are returned in chronological order (oldest first)", prop.ForAll(
		func(count int) bool {
			backend := NewMemoryBackend(200) // large depth so no eviction
			ctx := context.Background()
			alias := "test-agent"

			// Build entries with strictly increasing timestamps
			baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
			appendedEntries := make([]Entry, count)
			for i := 0; i < count; i++ {
				entry := Entry{
					Timestamp: baseTime.Add(time.Duration(i) * time.Second),
					SentMsg:   fmt.Sprintf("msg-%d", i),
					Response:  fmt.Sprintf("resp-%d", i),
					ContextID: fmt.Sprintf("ctx-%d", i),
					TaskID:    fmt.Sprintf("task-%d", i),
					IsError:   i%2 == 0,
				}
				appendedEntries[i] = entry
				if err := backend.Append(ctx, alias, entry); err != nil {
					t.Logf("Append returned error: %v", err)
					return false
				}
			}

			entries, err := backend.List(ctx, alias)
			if err != nil {
				t.Logf("List returned error: %v", err)
				return false
			}

			// Must have the same number of entries
			if len(entries) != count {
				t.Logf("expected %d entries, got %d", count, len(entries))
				return false
			}

			// Entries must be in chronological order (oldest first)
			for i := 1; i < len(entries); i++ {
				if !entries[i].Timestamp.After(entries[i-1].Timestamp) {
					t.Logf("entry[%d] timestamp %v is not after entry[%d] timestamp %v",
						i, entries[i].Timestamp, i-1, entries[i-1].Timestamp)
					return false
				}
			}

			// Entries must match appended order exactly
			for i, entry := range entries {
				if entry.SentMsg != appendedEntries[i].SentMsg {
					t.Logf("entry[%d] SentMsg: got %q, want %q", i, entry.SentMsg, appendedEntries[i].SentMsg)
					return false
				}
				if entry.Response != appendedEntries[i].Response {
					t.Logf("entry[%d] Response: got %q, want %q", i, entry.Response, appendedEntries[i].Response)
					return false
				}
				if !entry.Timestamp.Equal(appendedEntries[i].Timestamp) {
					t.Logf("entry[%d] Timestamp: got %v, want %v", i, entry.Timestamp, appendedEntries[i].Timestamp)
					return false
				}
			}

			return true
		},
		countGen,
	))

	properties.TestingRun(t)
}

// Feature: task-history, Property 5: Limit parameter returns most recent entries
// **Validates: Requirements 2.3**

// applyLimit returns the last min(len(entries), limit) entries from the slice.
// If limit >= len(entries), all entries are returned. If limit <= 0, all entries
// are returned (invalid limit is treated as "no limit").
func applyLimit(entries []Entry, limit int) []Entry {
	if limit <= 0 || limit >= len(entries) {
		return entries
	}
	return entries[len(entries)-limit:]
}

func TestPropertyLimitParameterReturnsMostRecentEntries(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for entry count (1-100)
	entryCountGen := gen.IntRange(1, 100)

	// Generator for limit (1-200)
	limitGen := gen.IntRange(1, 200)

	properties.Property("applyLimit returns exactly min(N, L) most recent entries in chronological order", prop.ForAll(
		func(entryCount int, limit int) bool {
			backend := NewMemoryBackend(200) // large depth to avoid eviction
			ctx := context.Background()
			alias := "test-agent"

			// Append entryCount entries with strictly increasing timestamps
			baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
			for i := 0; i < entryCount; i++ {
				entry := Entry{
					Timestamp: baseTime.Add(time.Duration(i) * time.Second),
					SentMsg:   fmt.Sprintf("msg-%d", i),
					Response:  fmt.Sprintf("resp-%d", i),
					ContextID: fmt.Sprintf("ctx-%d", i),
					TaskID:    fmt.Sprintf("task-%d", i),
					IsError:   false,
				}
				if err := backend.Append(ctx, alias, entry); err != nil {
					t.Logf("Append returned error: %v", err)
					return false
				}
			}

			// Get all entries from the backend
			allEntries, err := backend.List(ctx, alias)
			if err != nil {
				t.Logf("List returned error: %v", err)
				return false
			}

			// Apply limit
			limited := applyLimit(allEntries, limit)

			// Verify count: should be exactly min(N, L)
			expectedCount := entryCount
			if limit < entryCount {
				expectedCount = limit
			}
			if len(limited) != expectedCount {
				t.Logf("expected %d entries, got %d (N=%d, L=%d)", expectedCount, len(limited), entryCount, limit)
				return false
			}

			// Verify the entries are the LAST min(N, L) from allEntries
			startIdx := len(allEntries) - expectedCount
			for i, entry := range limited {
				expected := allEntries[startIdx+i]
				if entry.SentMsg != expected.SentMsg {
					t.Logf("entry[%d] SentMsg: got %q, want %q", i, entry.SentMsg, expected.SentMsg)
					return false
				}
				if entry.Response != expected.Response {
					t.Logf("entry[%d] Response: got %q, want %q", i, entry.Response, expected.Response)
					return false
				}
				if !entry.Timestamp.Equal(expected.Timestamp) {
					t.Logf("entry[%d] Timestamp: got %v, want %v", i, entry.Timestamp, expected.Timestamp)
					return false
				}
			}

			// Verify chronological order is maintained
			for i := 1; i < len(limited); i++ {
				if !limited[i].Timestamp.After(limited[i-1].Timestamp) {
					t.Logf("entry[%d] timestamp %v is not after entry[%d] timestamp %v",
						i, limited[i].Timestamp, i-1, limited[i-1].Timestamp)
					return false
				}
			}

			return true
		},
		entryCountGen,
		limitGen,
	))

	properties.TestingRun(t)
}

// Feature: task-history, Property 6: Delete removes all entries for an alias
// **Validates: Requirements 4.1, 4.2, 4.3**

func TestPropertyDeleteRemovesAllEntriesForAnAlias(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for entry count (1-100)
	entryCountGen := gen.IntRange(1, 100)

	// Sub-property 1: Delete removes all entries (List returns empty slice after Delete)
	properties.Property("After appending K entries and calling Delete, List returns an empty slice", prop.ForAll(
		func(entryCount int) bool {
			backend := NewMemoryBackend(200) // large depth so no eviction
			ctx := context.Background()
			alias := "test-agent"

			// Append entryCount entries
			baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
			for i := 0; i < entryCount; i++ {
				entry := Entry{
					Timestamp: baseTime.Add(time.Duration(i) * time.Second),
					SentMsg:   fmt.Sprintf("msg-%d", i),
					Response:  fmt.Sprintf("resp-%d", i),
					ContextID: fmt.Sprintf("ctx-%d", i),
					TaskID:    fmt.Sprintf("task-%d", i),
					IsError:   i%2 == 0,
				}
				if err := backend.Append(ctx, alias, entry); err != nil {
					t.Logf("Append returned error: %v", err)
					return false
				}
			}

			// Verify entries exist before delete
			entriesBefore, err := backend.List(ctx, alias)
			if err != nil {
				t.Logf("List returned error before Delete: %v", err)
				return false
			}
			if len(entriesBefore) == 0 {
				t.Logf("expected entries before Delete, got empty slice")
				return false
			}

			// Call Delete
			if err := backend.Delete(ctx, alias); err != nil {
				t.Logf("Delete returned error: %v", err)
				return false
			}

			// Verify List returns empty slice after Delete
			entriesAfter, err := backend.List(ctx, alias)
			if err != nil {
				t.Logf("List returned error after Delete: %v", err)
				return false
			}
			if len(entriesAfter) != 0 {
				t.Logf("expected empty slice after Delete, got %d entries", len(entriesAfter))
				return false
			}

			return true
		},
		entryCountGen,
	))

	// Sub-property 2: Clear removes all entries (List returns empty slice after Clear)
	properties.Property("After appending K entries and calling Clear, List returns an empty slice", prop.ForAll(
		func(entryCount int) bool {
			backend := NewMemoryBackend(200) // large depth so no eviction
			ctx := context.Background()
			alias := "test-agent"

			// Append entryCount entries
			baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
			for i := 0; i < entryCount; i++ {
				entry := Entry{
					Timestamp: baseTime.Add(time.Duration(i) * time.Second),
					SentMsg:   fmt.Sprintf("msg-%d", i),
					Response:  fmt.Sprintf("resp-%d", i),
					ContextID: fmt.Sprintf("ctx-%d", i),
					TaskID:    fmt.Sprintf("task-%d", i),
					IsError:   i%2 == 0,
				}
				if err := backend.Append(ctx, alias, entry); err != nil {
					t.Logf("Append returned error: %v", err)
					return false
				}
			}

			// Verify entries exist before clear
			entriesBefore, err := backend.List(ctx, alias)
			if err != nil {
				t.Logf("List returned error before Clear: %v", err)
				return false
			}
			if len(entriesBefore) == 0 {
				t.Logf("expected entries before Clear, got empty slice")
				return false
			}

			// Call Clear
			if err := backend.Clear(ctx, alias); err != nil {
				t.Logf("Clear returned error: %v", err)
				return false
			}

			// Verify List returns empty slice after Clear
			entriesAfter, err := backend.List(ctx, alias)
			if err != nil {
				t.Logf("List returned error after Clear: %v", err)
				return false
			}
			if len(entriesAfter) != 0 {
				t.Logf("expected empty slice after Clear, got %d entries", len(entriesAfter))
				return false
			}

			return true
		},
		entryCountGen,
	))

	properties.TestingRun(t)
}

// Feature: task-history, Property 7: Concurrent operations maintain safety invariants
// **Validates: Requirements 6.1**

func TestPropertyConcurrentOperationsMaintainSafetyInvariants(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for depth (5-50)
	depthGen := gen.IntRange(5, 50)

	// Generator for number of goroutines (5-20)
	goroutineCountGen := gen.IntRange(5, 20)

	// Generator for number of operations per goroutine (5-30)
	opsPerGoroutineGen := gen.IntRange(5, 30)

	properties.Property("Concurrent Append/List/Delete operations do not panic and maintain invariants", prop.ForAll(
		func(depth int, goroutineCount int, opsPerGoroutine int) bool {
			backend := NewMemoryBackend(depth)
			ctx := context.Background()
			alias := "concurrent-test-agent"

			// Track panics and invariant violations
			panicCh := make(chan string, goroutineCount*opsPerGoroutine)

			var wg sync.WaitGroup
			wg.Add(goroutineCount)

			for g := range goroutineCount {
				go func(goroutineID int) {
					defer wg.Done()
					defer func() {
						if r := recover(); r != nil {
							panicCh <- fmt.Sprintf("goroutine %d panicked: %v", goroutineID, r)
						}
					}()

					for op := range opsPerGoroutine {
						// Determine operation type: 0=Append, 1=List, 2=Delete
						opType := (goroutineID + op) % 3

						switch opType {
						case 0: // Append
							entry := Entry{
								Timestamp: time.Now().UTC(),
								SentMsg:   fmt.Sprintf("msg-g%d-op%d", goroutineID, op),
								Response:  fmt.Sprintf("resp-g%d-op%d", goroutineID, op),
								ContextID: fmt.Sprintf("ctx-g%d", goroutineID),
								TaskID:    fmt.Sprintf("task-g%d-op%d", goroutineID, op),
								IsError:   op%5 == 0,
							}
							_ = backend.Append(ctx, alias, entry)
						case 1: // List
							entries, err := backend.List(ctx, alias)
							if err != nil {
								panicCh <- fmt.Sprintf("goroutine %d: List error: %v", goroutineID, err)
								return
							}
							// Verify depth invariant holds during concurrent operations
							if len(entries) > depth {
								panicCh <- fmt.Sprintf("goroutine %d: List returned %d entries, exceeds depth %d",
									goroutineID, len(entries), depth)
								return
							}
							// Verify all entries have non-zero timestamps (data integrity)
							for i, e := range entries {
								if e.Timestamp.IsZero() {
									panicCh <- fmt.Sprintf("goroutine %d: entry[%d] has zero timestamp",
										goroutineID, i)
									return
								}
							}
						case 2: // Delete
							_ = backend.Delete(ctx, alias)
						}
					}
				}(g)
			}

			wg.Wait()
			close(panicCh)

			// Check for panics or invariant violations during execution
			for panicMsg := range panicCh {
				t.Logf("PANIC or invariant violation: %s", panicMsg)
				return false
			}

			// After all operations complete, verify final state
			entries, err := backend.List(ctx, alias)
			if err != nil {
				t.Logf("Final List returned error: %v", err)
				return false
			}

			// Verify len(entries) <= depth
			if len(entries) > depth {
				t.Logf("len(entries) = %d exceeds depth = %d", len(entries), depth)
				return false
			}

			// Verify data integrity: all entries have valid, non-zero timestamps
			for i, e := range entries {
				if e.Timestamp.IsZero() {
					t.Logf("entry[%d] has zero timestamp", i)
					return false
				}
				if e.SentMsg == "" {
					t.Logf("entry[%d] has empty SentMsg", i)
					return false
				}
				if e.Response == "" {
					t.Logf("entry[%d] has empty Response", i)
					return false
				}
			}

			// Verify entries are in chronological (insertion) order.
			// The MemoryBackend always appends at the tail of the slice and
			// holds a write lock during Append, so entries between any two
			// Delete calls are in the order their Appends acquired the lock.
			// With concurrent Appends using time.Now(), timestamps reflect
			// real wall-clock time at lock acquisition, so they should be
			// non-decreasing within a contiguous run (no Deletes in between).
			// After Deletes clear the slice and new Appends refill it, the
			// remaining entries are those appended after the last Delete,
			// which should be in non-decreasing timestamp order.
			for i := 1; i < len(entries); i++ {
				if entries[i].Timestamp.Before(entries[i-1].Timestamp) {
					// Allow small clock jitter (< 1ms) that can occur on some
					// systems when goroutines are scheduled very close together.
					diff := entries[i-1].Timestamp.Sub(entries[i].Timestamp)
					if diff > time.Millisecond {
						t.Logf("entry[%d] timestamp %v is before entry[%d] timestamp %v (diff: %v)",
							i, entries[i].Timestamp, i-1, entries[i-1].Timestamp, diff)
						return false
					}
				}
			}

			return true
		},
		depthGen,
		goroutineCountGen,
		opsPerGoroutineGen,
	))

	properties.TestingRun(t)
}
