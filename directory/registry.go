package directory

import (
	"context"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// Registry defines the storage contract for agent cards.
// Implementations MUST be safe for concurrent use from multiple goroutines.
type Registry interface {
	// Register adds or replaces an agent card. The card's Name field is the unique key.
	Register(ctx context.Context, card a2a.AgentCard) error
	// Unregister removes an agent card by name.
	// Returns true if the card was found and removed, false otherwise.
	Unregister(ctx context.Context, name string) (bool, error)
	// List returns all registered agent cards.
	List(ctx context.Context) ([]a2a.AgentCard, error)
	// Len returns the number of registered agent cards.
	Len(ctx context.Context) (int, error)
}

// Filterer is an optional interface that a Registry can implement to support
// native server-side filtering. If the Registry implements Filterer,
// the handler delegates filtering to it instead of using the FilterResolver.
type Filterer interface {
	Filter(ctx context.Context, filter string) ([]a2a.AgentCard, error)
}

// Compile-time interface check.
var _ Registry = (*MemoryRegistry)(nil)

// MemoryRegistry is a thread-safe, in-memory Registry backed by sync.RWMutex and a map.
// It does NOT implement Querier — filtering falls back to the QueryResolver.
type MemoryRegistry struct {
	mu    sync.RWMutex
	cards map[string]a2a.AgentCard // keyed by AgentCard.Name
}

// NewMemoryRegistry creates an empty in-memory registry.
func NewMemoryRegistry() *MemoryRegistry {
	return &MemoryRegistry{
		cards: make(map[string]a2a.AgentCard),
	}
}

// Register adds or replaces an agent card. The card's Name field is used as the key.
// MemoryRegistry never returns an error.
func (r *MemoryRegistry) Register(_ context.Context, card a2a.AgentCard) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.cards[card.Name] = card
	return nil
}

// Unregister removes an agent card by name.
// Returns true if the card was found and removed, false otherwise.
// MemoryRegistry never returns an error.
func (r *MemoryRegistry) Unregister(_ context.Context, name string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, ok := r.cards[name]
	if !ok {
		return false, nil
	}
	delete(r.cards, name)
	return true, nil
}

// List returns all registered agent cards.
// MemoryRegistry never returns an error.
func (r *MemoryRegistry) List(_ context.Context) ([]a2a.AgentCard, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cards := make([]a2a.AgentCard, 0, len(r.cards))
	for _, card := range r.cards {
		cards = append(cards, card)
	}
	return cards, nil
}

// Len returns the number of registered agent cards.
// MemoryRegistry never returns an error.
func (r *MemoryRegistry) Len(_ context.Context) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.cards), nil
}
