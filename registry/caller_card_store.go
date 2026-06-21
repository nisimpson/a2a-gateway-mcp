package registry

import "sync"

const defaultCallerCardKey = "caller_agent_card"

// CallerCardStore manages the stored caller card state.
// It is safe for concurrent access from multiple goroutines.
type CallerCardStore struct {
	mu   sync.RWMutex
	card *CallerCard
	key  string
}

// NewCallerCardStore creates an empty caller card store.
func NewCallerCardStore() *CallerCardStore {
	return &CallerCardStore{}
}

// Set stores the caller card with the given metadata key.
// If metadataKey is empty, the default key "caller_agent_card" is used.
func (s *CallerCardStore) Set(card *CallerCard, metadataKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.card = card
	if metadataKey != "" {
		s.key = metadataKey
	} else {
		s.key = defaultCallerCardKey
	}
}

// Get returns the stored caller card, or nil if none is registered.
func (s *CallerCardStore) Get() *CallerCard {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.card
}

// Remove clears the stored caller card. Returns true if there was a card to remove.
func (s *CallerCardStore) Remove() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.card == nil {
		return false
	}
	s.card = nil
	s.key = ""
	return true
}

// InjectCallerCard merges the stored caller card into the given metadata map.
// Returns the (possibly new) metadata map. Does not overwrite if the key already exists.
func (s *CallerCardStore) InjectCallerCard(metadata map[string]any) map[string]any {
	s.mu.RLock()
	card := s.card
	key := s.key
	s.mu.RUnlock()

	if card == nil {
		return metadata
	}
	if key == "" {
		key = defaultCallerCardKey
	}

	if metadata != nil {
		if _, exists := metadata[key]; exists {
			return metadata
		}
	}

	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata[key] = card
	return metadata
}
