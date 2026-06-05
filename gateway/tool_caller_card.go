package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultCallerCardKey = "caller_agent_card"

// handleCreateCallerCard registers or replaces the caller agent card.
func (s *Server) handleCreateCallerCard(ctx context.Context, _ *mcp.CallToolRequest, input CreateCallerCardInput) (*mcp.CallToolResult, any, error) {
	// Requirement: CAC-1.8 — reject empty/whitespace-only name
	if strings.TrimSpace(input.Name) == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "name must not be empty or whitespace-only"}},
			IsError: true,
		}, nil, nil
	}

	card := &CallerCard{
		Name:         input.Name,
		Description:  input.Description,
		URL:          input.URL,
		Skills:       input.Skills,
		Capabilities: input.Capabilities,
	}

	// Requirement: CAC-1.9, CAC-1.10 — store card globally, replacing any existing
	s.callerCardMu.Lock()
	s.callerCard = card
	s.callerCardKey = input.MetadataKey
	s.callerCardMu.Unlock()

	// Requirement: CAC-1.11 — return confirmation
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Caller agent card registered for %q", input.Name)}},
	}, nil, nil
}

// Requirements: CAC-3.1, CAC-3.2, CAC-3.3

// handleViewCallerCard returns the currently stored caller agent card as JSON.
func (s *Server) handleViewCallerCard(ctx context.Context, _ *mcp.CallToolRequest, input ViewCallerCardInput) (*mcp.CallToolResult, any, error) {
	s.callerCardMu.RLock()
	card := s.callerCard
	s.callerCardMu.RUnlock()

	if card == nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "no caller agent card is currently set"}},
		}, nil, nil
	}

	data, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to serialize caller card: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

// Requirements: CAC-3.4, CAC-3.5, CAC-3.6

// handleRemoveCallerCard clears the stored caller agent card.
func (s *Server) handleRemoveCallerCard(ctx context.Context, _ *mcp.CallToolRequest, input RemoveCallerCardInput) (*mcp.CallToolResult, any, error) {
	s.callerCardMu.Lock()
	had := s.callerCard != nil
	s.callerCard = nil
	s.callerCardKey = ""
	s.callerCardMu.Unlock()

	if !had {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "no caller agent card was set"}},
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "Caller agent card removed"}},
	}, nil, nil
}

// Requirements: CAC-2.1, CAC-2.3, CAC-2.4, CAC-2.5

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
