package gateway

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// listAgentEntry is the JSON representation of an agent in the list response.
type listAgentEntry struct {
	Alias string `json:"alias"`
	URL   string `json:"url"`
}

// handleListAgents returns a JSON array of all currently connected agents
// with their aliases and URLs, sorted by alias in ascending order.
func (s *Server) handleListAgents(_ context.Context, _ *mcp.CallToolRequest, _ ListAgentsInput) (*mcp.CallToolResult, any, error) {
	entries := s.registry.List()

	result := make([]listAgentEntry, len(entries))
	for i, entry := range entries {
		result[i] = listAgentEntry{
			Alias: entry.Alias,
			URL:   entry.URL,
		}
	}

	data, err := json.Marshal(result)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "failed to serialize agent list: " + err.Error()}},
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}
