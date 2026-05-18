package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// handleGetAgentCard retrieves the agent card from an A2A agent's
// /.well-known/agent.json endpoint.
func (s *Server) handleGetAgentCard(ctx context.Context, _ *mcp.CallToolRequest, input GetAgentCardInput) (*mcp.CallToolResult, any, error) {
	// Validate agent identifier is non-empty.
	if strings.TrimSpace(input.Agent) == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "agent identifier is required and cannot be empty"}},
		}, nil, nil
	}

	// Resolve agent identifier using registry or URL validation.
	resolved, err := ResolveAgent(s.registry, input.Agent)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to resolve agent: %s", err.Error())}},
		}, nil, nil
	}

	// Determine which HTTP client to use.
	// If resolved from alias, use httpClientForAgent to inject stored headers.
	// Otherwise, use the server's base HTTP client directly.
	client := s.httpClient
	if resolved.IsAlias {
		entry := s.registry.Lookup(input.Agent)
		if entry != nil {
			client = httpClientForAgent(s.httpClient, entry)
		}
	}

	// Build the agent card URL.
	agentCardURL := strings.TrimRight(resolved.URL, "/") + "/.well-known/agent.json"

	// Create the HTTP GET request.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, agentCardURL, nil)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to create request: %s", err.Error())}},
		}, nil, nil
	}

	// Send the request.
	resp, err := client.Do(req)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("agent unreachable: %s", err.Error())}},
		}, nil, nil
	}
	defer resp.Body.Close()

	// Check for non-200 status.
	if resp.StatusCode != http.StatusOK {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("agent returned non-200 status: %d", resp.StatusCode)}},
		}, nil, nil
	}

	// Read the response body.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to read response body: %s", err.Error())}},
		}, nil, nil
	}

	// Validate that the response is valid JSON.
	var jsonObj any
	if err := json.Unmarshal(body, &jsonObj); err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to parse agent card JSON: %s", err.Error())}},
		}, nil, nil
	}

	// Return the raw JSON as MCP text content.
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, nil, nil
}
