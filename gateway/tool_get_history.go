package gateway

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetHistoryInput is the input schema for the get_history tool.
type GetHistoryInput struct {
	Agent string `json:"agent" jsonschema:"agent alias to retrieve history for"`
	Limit *int   `json:"limit,omitempty" jsonschema:"maximum number of recent entries to return"`
}

// handleGetHistory retrieves the interaction history for a registered agent.
func (s *Server) handleGetHistory(ctx context.Context, _ *mcp.CallToolRequest, input GetHistoryInput) (*mcp.CallToolResult, any, error) {
	// Validate alias is non-empty.
	if input.Agent == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "agent alias is required"}},
		}, nil, nil
	}

	// Validate alias is registered in the agent registry.
	if entry := s.registry.Lookup(input.Agent); entry == nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "agent not found in registry"}},
		}, nil, nil
	}

	// Retrieve history entries from the backend.
	entries, err := s.historyBackend.List(ctx, input.Agent)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}

	// Apply limit if provided and positive — return the most recent N entries.
	if input.Limit != nil && *input.Limit > 0 && len(entries) > *input.Limit {
		entries = entries[len(entries)-*input.Limit:]
	}

	// Serialize entries as JSON array. Empty slice produces "[]".
	data, err := json.Marshal(entries)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "failed to serialize history: " + err.Error()}},
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}
