package history

import (
	"context"
	"sync"
)

// Compile-time check that MemoryBackend implements HistoryBackend.
var _ Backend = (*MemoryBackend)(nil)

// MemoryBackend stores history entries in process memory with a fixed
// depth limit per alias. It is safe for concurrent access.
type MemoryBackend struct {
	mu      sync.RWMutex
	entries map[string][]Entry
	depth   int
}

// NewMemoryBackend creates an in-memory backend with the given depth limit.
func NewMemoryBackend(depth int) *MemoryBackend {
	return &MemoryBackend{
		entries: make(map[string][]Entry),
		depth:   depth,
	}
}

// Append adds a new entry to the history for the given alias.
// If the alias is at capacity (depth), the oldest entry is evicted before appending.
func (m *MemoryBackend) Append(_ context.Context, alias string, entry Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries := m.entries[alias]
	if len(entries) >= m.depth {
		// Evict oldest entry (index 0).
		entries = entries[1:]
	}
	m.entries[alias] = append(entries, entry)
	return nil
}

// List returns all stored entries for the alias in chronological order (oldest first).
// Returns a copy of the slice to prevent external mutation.
func (m *MemoryBackend) List(_ context.Context, alias string) ([]Entry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries := m.entries[alias]
	if len(entries) == 0 {
		return []Entry{}, nil
	}

	cp := make([]Entry, len(entries))
	copy(cp, entries)
	return cp, nil
}

// Delete removes all history entries for the given alias.
func (m *MemoryBackend) Delete(_ context.Context, alias string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.entries, alias)
	return nil
}

// Clear removes all history entries for the given alias.
// Semantically identical to Delete — provided for naming clarity at call sites.
func (m *MemoryBackend) Clear(_ context.Context, alias string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.entries, alias)
	return nil
}

// Close is a no-op for the in-memory backend (nothing to clean up).
func (m *MemoryBackend) Close(_ context.Context) error {
	return nil
}
