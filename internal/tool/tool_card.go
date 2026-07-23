package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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

func (g *GetAgentCardTool) Handle(ctx context.Context, _ *mcp.CallToolRequest, input *GetAgentCardInput) (*mcp.CallToolResult, *GetAgentCardOutput, error) {
	if strings.TrimSpace(input.Agent) == "" {
		return nil, nil, errors.New("agent identifier is required and cannot be empty")
	}

	resolved, err := g.AgentRegistry.ResolveAgent(input.Agent)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve agent: %w", err)
	}

	// Build the agent card URL.
	agentCardURL := strings.TrimRight(resolved.URL, "/") + "/.well-known/agent-card.json"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, agentCardURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Inject stored headers for alias-resolved agents.
	if resolved.IsAlias {
		for k, v := range resolved.Headers {
			req.Header.Set(k, v)
		}
	}

	resp, err := g.HTTPClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("agent unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("agent returned non-200 status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse into output struct
	var output GetAgentCardOutput
	if err := json.Unmarshal(body, &output); err != nil {
		return nil, nil, fmt.Errorf("failed to parse agent card JSON: %w", err)
	}

	return nil, &output, nil
}

func NewGetAgentCardTool(env *Env) *GetAgentCardTool {
	return &GetAgentCardTool{
		AgentRegistry: env.AgentRegistry,
		HTTPClient:    env.HTTPDoer,
	}
}
