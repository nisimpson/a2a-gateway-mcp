package gateway

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// handleDisconnectAgent removes a registered agent by alias from the gateway registry.
func (s *Server) handleDisconnectAgent(_ context.Context, _ *mcp.CallToolRequest, input DisconnectAgentInput) (*mcp.CallToolResult, any, error) {
	// Validate alias is non-empty.
	if err := ValidateAlias(input.Alias); err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}

	// Remove registry entry.
	entry := s.registry.Disconnect(input.Alias)
	if entry == nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{
				Text: fmt.Sprintf("agent %q not found in registry", input.Alias),
			}},
		}, nil, nil
	}

	// Evict cached SDK client so reconnection uses a fresh client.
	s.clients.Evict(entry.URL)

	// Remove rate limiter for this agent.
	s.rateLimiters.Remove(input.Alias)

	// Also delete context store entry.
	s.contextStore.Delete(input.Alias)

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{
			Text: fmt.Sprintf("Disconnected agent %q", input.Alias),
		}},
	}, nil, nil
}
