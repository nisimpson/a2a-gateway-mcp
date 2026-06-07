package gateway

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ClearHistoryInput is the input schema for the clear_history tool.
type ClearHistoryInput struct {
	Agent string `json:"agent" jsonschema:"agent alias to clear history for"`
}

// handleClearHistory clears all interaction history for a registered agent.
func (s *Server) handleClearHistory(ctx context.Context, _ *mcp.CallToolRequest, input ClearHistoryInput) (*mcp.CallToolResult, any, error) {
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

	// Clear history entries for this agent.
	if err := s.historyBackend.Clear(ctx, input.Agent); err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Cleared history for agent %q", input.Agent)}},
	}, nil, nil
}
