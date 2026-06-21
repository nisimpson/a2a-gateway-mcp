package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetAgentCardInput is the input schema for the get_agent_card tool.
type GetAgentCardInput struct {
	Agent string `json:"agent" jsonschema:"agent alias from registry or full HTTP/HTTPS URL"`
}

// GetAgentCardTool retrieves the agent card from an A2A agent's
// /.well-known/agent-card.json endpoint.
type GetAgentCardTool struct {
	AgentRegistry AgentRegistry
	HTTPClient    HTTPDoer
}

func (g *GetAgentCardTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "get_agent_card",
		Description: "Retrieve the agent card from an A2A agent to discover its capabilities",
	}
}

func (g *GetAgentCardTool) Handle(ctx context.Context, _ *mcp.CallToolRequest, input *GetAgentCardInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(input.Agent) == "" {
		return toolError("agent identifier is required and cannot be empty"), nil, nil
	}

	resolved, err := g.AgentRegistry.ResolveAgent(input.Agent)
	if err != nil {
		return toolError(fmt.Sprintf("failed to resolve agent: %s", err.Error())), nil, nil
	}

	// Build the agent card URL.
	agentCardURL := strings.TrimRight(resolved.URL, "/") + "/.well-known/agent-card.json"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, agentCardURL, nil)
	if err != nil {
		return toolError(fmt.Sprintf("failed to create request: %s", err.Error())), nil, nil
	}

	// Inject stored headers for alias-resolved agents.
	if resolved.IsAlias {
		for k, v := range resolved.Headers {
			req.Header.Set(k, v)
		}
	}

	resp, err := g.HTTPClient.Do(req)
	if err != nil {
		return toolError(fmt.Sprintf("agent unreachable: %s", err.Error())), nil, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return toolError(fmt.Sprintf("agent returned non-200 status: %d", resp.StatusCode)), nil, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return toolError(fmt.Sprintf("failed to read response body: %s", err.Error())), nil, nil
	}

	// Validate that the response is valid JSON.
	var jsonObj any
	if err := json.Unmarshal(body, &jsonObj); err != nil {
		return toolError(fmt.Sprintf("failed to parse agent card JSON: %s", err.Error())), nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, nil, nil
}
