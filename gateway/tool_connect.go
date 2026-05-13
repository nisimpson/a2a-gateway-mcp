package gateway

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// handleConnectAgent registers a remote A2A agent with a friendly alias.
// Requirement: AGMCP-9.1, AGMCP-9.2, AGMCP-13.9, AGMCP-14.1 — connect with overwrite and context clearing
func (s *Server) handleConnectAgent(_ context.Context, _ *mcp.CallToolRequest, input ConnectAgentInput) (*mcp.CallToolResult, any, error) {
	// Validate alias format.
	if err := ValidateAlias(input.Alias); err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}

	// Validate URL scheme.
	if err := ValidateURL(input.AgentURL); err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}

	// Validate headers count.
	if err := ValidateHeaders(input.Headers); err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}

	// Check if alias already exists with a different URL; if so, clear context.
	existing := s.registry.Lookup(input.Alias)
	if existing != nil && existing.URL != input.AgentURL {
		s.contextStore.Delete(input.Alias)
	}

	// Add or update the registry entry.
	s.registry.Connect(input.Alias, input.AgentURL, input.Headers)

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{
			Text: fmt.Sprintf("Connected agent %q at %s", input.Alias, input.AgentURL),
		}},
	}, nil, nil
}
