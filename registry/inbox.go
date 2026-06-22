package registry

import (
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// InboxEntry represents a single item in the async inbox, containing the
// response from an agent along with metadata for identification.
type InboxEntry struct {
	Alias     string       // agent alias that produced this response
	TaskID    string       // A2A task ID (empty if dispatch failed)
	ContextID string       // A2A context ID for conversation continuity
	State     string       // terminal task state (completed, input-required, failed, error)
	Timestamp time.Time    // when the entry was deposited
	Task      *a2a.Task    // full task payload (nil if message response or error)
	Message   *a2a.Message // full message payload (nil if task response or error)
	Error     string       // non-empty if background dispatch failed
}

// InboxPeekFilter controls which entries are returned by a non-destructive peek.
type InboxPeekFilter struct {
	Alias string // optional; empty means all agents
}

// InboxPopOptions controls which entries are removed and returned by a destructive read.
type InboxPopOptions struct {
	Alias  string // required; which agent's messages to read
	Length *int   // optional; max entries to return (nil = all)
	Latest bool   // if true, remove all but return only the most recent
}

// MemoryInbox is an in-memory implementation of the inbox that stores entries
// in a mutex-guarded slice with configurable TTL-based expiration.
type MemoryInbox struct {
	mu      sync.Mutex
	entries []InboxEntry
	ttl     time.Duration
}

// NewMemoryInbox creates a new MemoryInbox with the given TTL.
// Entries older than TTL are lazily pruned during Peek and Pop operations.
func NewMemoryInbox(ttl time.Duration) *MemoryInbox {
	return &MemoryInbox{
		entries: make([]InboxEntry, 0),
		ttl:     ttl,
	}
}

// Deposit appends an entry to the inbox. If the entry's Timestamp is zero,
// it is set to the current time. Thread-safe.
func (m *MemoryInbox) Deposit(entry InboxEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	m.entries = append(m.entries, entry)
}

// Peek returns entries matching the filter without removing them.
// Expired entries (older than TTL) are pruned lazily during this call.
func (m *MemoryInbox) Peek(filter InboxPeekFilter) []InboxEntry {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.pruneExpiredLocked()

	result := make([]InboxEntry, 0)
	for _, e := range m.entries {
		if filter.Alias == "" || e.Alias == filter.Alias {
			result = append(result, e)
		}
	}
	return result
}

// Pop removes and returns entries matching the options.
// Applies Length/Latest semantics:
//   - Latest: true → removes all entries for the alias, returns only the most recent
//   - Length set → returns at most N entries in FIFO order, removes only those returned
//   - Neither → returns and removes all entries for the alias
//
// Expired entries are pruned lazily during this call.
func (m *MemoryInbox) Pop(opts InboxPopOptions) []InboxEntry {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.pruneExpiredLocked()

	// Partition entries into matched (for this alias) and remaining.
	var matched []InboxEntry
	var remaining []InboxEntry
	for _, e := range m.entries {
		if e.Alias == opts.Alias {
			matched = append(matched, e)
		} else {
			remaining = append(remaining, e)
		}
	}

	if len(matched) == 0 {
		return nil
	}

	var result []InboxEntry

	switch {
	case opts.Latest:
		// Remove all entries for the alias, return only the most recent.
		result = []InboxEntry{matched[len(matched)-1]}
		m.entries = remaining

	case opts.Length != nil:
		// Return at most N entries in FIFO order, remove only those returned.
		n := *opts.Length
		if n > len(matched) {
			n = len(matched)
		}
		result = matched[:n]
		// Keep the un-popped matched entries.
		kept := matched[n:]
		// Rebuild entries: remaining + kept (preserving chronological order).
		m.entries = make([]InboxEntry, 0, len(remaining)+len(kept))
		ri, ki := 0, 0
		for ri < len(remaining) || ki < len(kept) {
			if ri >= len(remaining) {
				m.entries = append(m.entries, kept[ki:]...)
				break
			}
			if ki >= len(kept) {
				m.entries = append(m.entries, remaining[ri:]...)
				break
			}
			// Merge in chronological order by timestamp, with ties preserving original order.
			if !remaining[ri].Timestamp.After(kept[ki].Timestamp) {
				m.entries = append(m.entries, remaining[ri])
				ri++
			} else {
				m.entries = append(m.entries, kept[ki])
				ki++
			}
		}

	default:
		// Return and remove all entries for the alias.
		result = matched
		m.entries = remaining
	}

	return result
}

// pruneExpiredLocked removes entries older than TTL from the slice.
// Must be called with m.mu held.
func (m *MemoryInbox) pruneExpiredLocked() {
	if m.ttl <= 0 {
		return
	}
	cutoff := time.Now().Add(-m.ttl)
	n := 0
	for _, e := range m.entries {
		if !e.Timestamp.Before(cutoff) {
			m.entries[n] = e
			n++
		}
	}
	m.entries = m.entries[:n]
}
