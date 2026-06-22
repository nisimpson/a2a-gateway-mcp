package registry

import (
	"sync"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// Feature: async-inbox, Property 1: FIFO Ordering
// **Validates: Requirements AINB-4.3**

func TestPropertyFIFOOrdering(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	aliasGen := gen.RegexMatch(`[a-z][a-z0-9]{1,8}`)

	properties.Property("entries deposited in order are returned in that same order by Peek", prop.ForAll(
		func(alias string, n int) bool {
			inbox := NewMemoryInbox(time.Hour)

			// Deposit n entries with sequential timestamps.
			base := time.Now()
			for i := 0; i < n; i++ {
				inbox.Deposit(InboxEntry{
					Alias:     alias,
					TaskID:    taskID(i),
					Timestamp: base.Add(time.Duration(i) * time.Second),
				})
			}

			// Peek should return them in the same order.
			results := inbox.Peek(InboxPeekFilter{Alias: alias})
			if len(results) != n {
				return false
			}
			for i, e := range results {
				if e.TaskID != taskID(i) {
					return false
				}
			}
			return true
		},
		aliasGen,
		gen.IntRange(1, 50),
	))

	properties.Property("entries deposited in order are returned in that same order by Pop", prop.ForAll(
		func(alias string, n int) bool {
			inbox := NewMemoryInbox(time.Hour)

			base := time.Now()
			for i := 0; i < n; i++ {
				inbox.Deposit(InboxEntry{
					Alias:     alias,
					TaskID:    taskID(i),
					Timestamp: base.Add(time.Duration(i) * time.Second),
				})
			}

			results := inbox.Pop(InboxPopOptions{Alias: alias})
			if len(results) != n {
				return false
			}
			for i, e := range results {
				if e.TaskID != taskID(i) {
					return false
				}
			}
			return true
		},
		aliasGen,
		gen.IntRange(1, 50),
	))

	properties.TestingRun(t)
}

// Feature: async-inbox, Property 2: Exactly-Once Delivery
// **Validates: Requirements AINB-4.1, AINB-4.3**

func TestPropertyExactlyOnceDelivery(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	aliasGen := gen.RegexMatch(`[a-z][a-z0-9]{1,8}`)

	properties.Property("after Pop, entries are no longer visible to Peek or Pop", prop.ForAll(
		func(alias string, n int) bool {
			inbox := NewMemoryInbox(time.Hour)

			base := time.Now()
			for i := 0; i < n; i++ {
				inbox.Deposit(InboxEntry{
					Alias:     alias,
					TaskID:    taskID(i),
					Timestamp: base.Add(time.Duration(i) * time.Second),
				})
			}

			// Pop all entries.
			popped := inbox.Pop(InboxPopOptions{Alias: alias})
			if len(popped) != n {
				return false
			}

			// Peek should return nothing for this alias.
			peekAfter := inbox.Peek(InboxPeekFilter{Alias: alias})
			if len(peekAfter) != 0 {
				return false
			}

			// Pop again should return nothing.
			popAfter := inbox.Pop(InboxPopOptions{Alias: alias})
			return len(popAfter) == 0
		},
		aliasGen,
		gen.IntRange(1, 30),
	))

	properties.TestingRun(t)
}

// Feature: async-inbox, Property 3: Non-Destructive Peek
// **Validates: Requirements AINB-3.6**

func TestPropertyNonDestructivePeek(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	aliasGen := gen.RegexMatch(`[a-z][a-z0-9]{1,8}`)

	properties.Property("Peek does not alter entries; calling Peek multiple times returns the same result", prop.ForAll(
		func(alias string, n int, peekCount int) bool {
			inbox := NewMemoryInbox(time.Hour)

			base := time.Now()
			for i := 0; i < n; i++ {
				inbox.Deposit(InboxEntry{
					Alias:     alias,
					TaskID:    taskID(i),
					Timestamp: base.Add(time.Duration(i) * time.Second),
				})
			}

			// Peek multiple times and verify results are identical.
			filter := InboxPeekFilter{Alias: alias}
			first := inbox.Peek(filter)
			for attempt := 0; attempt < peekCount; attempt++ {
				current := inbox.Peek(filter)
				if len(current) != len(first) {
					return false
				}
				for i := range first {
					if current[i].TaskID != first[i].TaskID {
						return false
					}
					if current[i].Alias != first[i].Alias {
						return false
					}
				}
			}

			return true
		},
		aliasGen,
		gen.IntRange(1, 20),
		gen.IntRange(2, 10),
	))

	properties.TestingRun(t)
}

// Feature: async-inbox, Property 4: TTL Expiration
// **Validates: Requirements AINB-5.2**

func TestPropertyTTLExpiration(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	aliasGen := gen.RegexMatch(`[a-z][a-z0-9]{1,8}`)

	properties.Property("entries older than TTL are not returned by Peek or Pop", prop.ForAll(
		func(alias string, freshCount int, expiredCount int) bool {
			ttl := 5 * time.Minute
			inbox := NewMemoryInbox(ttl)

			now := time.Now()

			// Deposit expired entries (timestamp older than TTL).
			for i := 0; i < expiredCount; i++ {
				inbox.Deposit(InboxEntry{
					Alias:     alias,
					TaskID:    "expired-" + taskID(i),
					Timestamp: now.Add(-ttl - time.Duration(i+1)*time.Second),
				})
			}

			// Deposit fresh entries (within TTL).
			for i := 0; i < freshCount; i++ {
				inbox.Deposit(InboxEntry{
					Alias:     alias,
					TaskID:    "fresh-" + taskID(i),
					Timestamp: now.Add(-time.Duration(i) * time.Second),
				})
			}

			// Peek should only return fresh entries.
			peeked := inbox.Peek(InboxPeekFilter{Alias: alias})
			if len(peeked) != freshCount {
				return false
			}
			for _, e := range peeked {
				if e.Timestamp.Before(now.Add(-ttl)) {
					return false
				}
			}

			// Pop should only return fresh entries.
			popped := inbox.Pop(InboxPopOptions{Alias: alias})
			if len(popped) != freshCount {
				return false
			}
			for _, e := range popped {
				if e.Timestamp.Before(now.Add(-ttl)) {
					return false
				}
			}

			return true
		},
		aliasGen,
		gen.IntRange(0, 20),
		gen.IntRange(1, 20),
	))

	properties.TestingRun(t)
}

// Feature: async-inbox, Property 7: Latest Semantics
// **Validates: Requirements AINB-4.5**

func TestPropertyLatestSemantics(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	aliasGen := gen.RegexMatch(`[a-z][a-z0-9]{1,8}`)

	properties.Property("Pop with Latest removes all entries but returns only the most recent one", prop.ForAll(
		func(alias string, n int) bool {
			inbox := NewMemoryInbox(time.Hour)

			base := time.Now()
			for i := 0; i < n; i++ {
				inbox.Deposit(InboxEntry{
					Alias:     alias,
					TaskID:    taskID(i),
					Timestamp: base.Add(time.Duration(i) * time.Second),
				})
			}

			// Pop with Latest: true.
			results := inbox.Pop(InboxPopOptions{
				Alias:  alias,
				Latest: true,
			})

			// Should return exactly 1 entry.
			if len(results) != 1 {
				return false
			}

			// The returned entry should be the most recent one.
			if results[0].TaskID != taskID(n-1) {
				return false
			}

			// All entries should be removed — Peek returns nothing.
			remaining := inbox.Peek(InboxPeekFilter{Alias: alias})
			return len(remaining) == 0
		},
		aliasGen,
		gen.IntRange(1, 50),
	))

	properties.TestingRun(t)
}

// Feature: async-inbox, Property 5: Async Independence (Concurrent Deposit Safety)
// **Validates: Requirements AINB-6.3**

func TestPropertyConcurrentDepositSafety(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	properties.Property("multiple goroutines depositing concurrently produce no data races and all entries are stored", prop.ForAll(
		func(goroutines int, entriesPerGoroutine int) bool {
			inbox := NewMemoryInbox(time.Hour)

			var wg sync.WaitGroup
			wg.Add(goroutines)

			for g := 0; g < goroutines; g++ {
				go func(gIdx int) {
					defer wg.Done()
					for i := 0; i < entriesPerGoroutine; i++ {
						inbox.Deposit(InboxEntry{
							Alias:  "agent",
							TaskID: gTaskID(gIdx, i),
						})
					}
				}(g)
			}

			wg.Wait()

			// Verify total count matches expected.
			all := inbox.Peek(InboxPeekFilter{Alias: "agent"})
			expected := goroutines * entriesPerGoroutine
			if len(all) != expected {
				return false
			}

			// Verify all expected task IDs are present.
			seen := make(map[string]bool, expected)
			for _, e := range all {
				seen[e.TaskID] = true
			}
			for g := 0; g < goroutines; g++ {
				for i := 0; i < entriesPerGoroutine; i++ {
					if !seen[gTaskID(g, i)] {
						return false
					}
				}
			}

			return true
		},
		gen.IntRange(2, 20),
		gen.IntRange(1, 10),
	))

	properties.TestingRun(t)
}

// Helper functions for generating predictable task IDs.

func taskID(i int) string {
	return "task-" + itoa(i)
}

func gTaskID(g, i int) string {
	return "g" + itoa(g) + "-task-" + itoa(i)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}
