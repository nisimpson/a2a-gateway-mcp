package registry

import (
	"sort"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// RegisteredAgent represents a registered agent in the registry.
type RegisteredAgent struct {
	Alias        string            // unique identifier for this agent in the registry
	URL          string            // base URL of the remote agent
	Headers      map[string]string // custom HTTP headers sent with every request to this agent
	Card         *a2a.AgentCard    // cached agent card; nil if not yet fetched or fetch failed
	PingEndpoint string            // optional relative path (e.g. "/healthz") for connectivity checks
}

// AgentRegistry is a thread-safe, in-memory map of aliases to agent entries.
type AgentRegistry struct {
	mu      sync.RWMutex
	entries map[string]*RegisteredAgent // key: alias
}

// NewAgentRegistry creates an empty registry.
func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{
		entries: make(map[string]*RegisteredAgent),
	}
}

// Connect adds or updates an agent entry. Returns true if the entry was
// updated (alias already existed), false if it was newly added.
// The pingEndpoint is optional — an empty string means "use default endpoint (.well-known/agent-card.json)".
func (r *AgentRegistry) Connect(alias, url string, headers map[string]string, pingEndpoint string) (updated bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, updated = r.entries[alias]
	r.entries[alias] = &RegisteredAgent{
		Alias:        alias,
		URL:          url,
		Headers:      headers,
		PingEndpoint: pingEndpoint,
	}
	return updated
}

// Disconnect removes an agent entry. Returns the removed entry or nil if
// the alias was not found.
func (r *AgentRegistry) Disconnect(alias string) *RegisteredAgent {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.entries[alias]
	if !ok {
		return nil
	}
	delete(r.entries, alias)
	return entry
}

// Lookup returns the entry for the given alias, or nil if not found.
func (r *AgentRegistry) Lookup(alias string) *RegisteredAgent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.entries[alias]
}

// List returns all entries sorted by alias in ascending lexicographic order.
func (r *AgentRegistry) List() []*RegisteredAgent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries := make([]*RegisteredAgent, 0, len(r.entries))
	for _, entry := range r.entries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Alias < entries[j].Alias
	})
	return entries
}

// SetCard stores the AgentCard for the given alias. Returns false if the
// alias is not found in the registry.
func (r *AgentRegistry) SetCard(alias string, card *a2a.AgentCard) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.entries[alias]
	if !ok {
		return false
	}
	entry.Card = card
	return true
}

// Len returns the number of registered agents.
func (r *AgentRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.entries)
}
