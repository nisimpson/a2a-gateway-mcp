package gateway

const defaultCallerCardKey = "caller_agent_card"

// CallerCard is the stored representation of the caller agent card.
type CallerCard struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url,omitempty"`
}

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
