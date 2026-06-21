package registry

import "sync"

// ContextStore is a thread-safe, in-memory map of agent aliases to context IDs.
type ContextStore struct {
	mu       sync.RWMutex
	contexts map[string]string // key: alias, value: context_id
}

// NewContextStore creates an empty context store.
func NewContextStore() *ContextStore {
	return &ContextStore{
		contexts: make(map[string]string),
	}
}

// Get returns the stored context ID for the alias, or empty string if none.
func (cs *ContextStore) Get(alias string) string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	return cs.contexts[alias]
}

// Set stores or updates the context ID for the alias.
// If contextID is empty, the entry is not modified.
func (cs *ContextStore) Set(alias, contextID string) {
	if contextID == "" {
		return
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.contexts[alias] = contextID
}

// Delete removes the context entry for the alias.
func (cs *ContextStore) Delete(alias string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	delete(cs.contexts, alias)
}
