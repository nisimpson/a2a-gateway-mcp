package gateway

import "github.com/nisimpson/a2a-gateway-mcp/registry"

const defaultCallerCardKey = "caller_agent_card"

// injectCallerCard merges the stored caller card into the given metadata map.
// Returns the (possibly new) metadata map. Does not overwrite if the key already exists.
func (s *Server) injectCallerCard(metadata map[string]any) map[string]any {
	s.callerCardMu.RLock()
	card := s.callerCard
	key := s.callerCardKey
	s.callerCardMu.RUnlock()

	if card == nil {
		return metadata
	}
	if key == "" {
		key = defaultCallerCardKey
	}

	// User-provided metadata takes precedence.
	if metadata != nil {
		if _, exists := metadata[key]; exists {
			return metadata
		}
	}

	// Initialize metadata map if nil.
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata[key] = card
	return metadata
}

// callerCardStore satisfies tool.CallerCardStore using the server's state.
type callerCardStore struct {
	server *Server
}

func (a *callerCardStore) Set(card *registry.CallerCard, metadataKey string) {
	a.server.callerCardMu.Lock()
	defer a.server.callerCardMu.Unlock()

	a.server.callerCard = card
	if metadataKey != "" {
		a.server.callerCardKey = metadataKey
	} else {
		a.server.callerCardKey = defaultCallerCardKey
	}
}

func (a *callerCardStore) Get() *registry.CallerCard {
	a.server.callerCardMu.RLock()
	defer a.server.callerCardMu.RUnlock()

	return a.server.callerCard
}

func (a *callerCardStore) Remove() bool {
	a.server.callerCardMu.Lock()
	defer a.server.callerCardMu.Unlock()

	if a.server.callerCard == nil {
		return false
	}
	a.server.callerCard = nil
	a.server.callerCardKey = ""
	return true
}
