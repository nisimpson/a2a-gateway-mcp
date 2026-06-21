package tool

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nisimpson/a2a-gateway-mcp/internal/validate"
)

// DisconnectAgentInput is the input schema for the disconnect_agent tool.
type DisconnectAgentInput struct {
	Alias string `json:"alias" jsonschema:"alias of the agent to disconnect"`
}

// DisconnectAgentTool removes a registered agent by alias.
type DisconnectAgentTool struct {
	AgentRegistry     AgentRegistry
	A2AClientResolver A2AClientResolver
	ContextStore      ContextStore
	RateLimiter       RateLimiter
	HealthTracker     HealthTracker
	HistoryBackend    HistoryBackend // nil if history is disabled
}

func (d *DisconnectAgentTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "disconnect_agent",
		Description: "Remove a registered agent by alias from the gateway registry",
	}
}

func (d *DisconnectAgentTool) Handle(ctx context.Context, _ *mcp.CallToolRequest, input *DisconnectAgentInput) (*mcp.CallToolResult, any, error) {
	if err := validate.ValidateAlias(input.Alias); err != nil {
		return toolError(err.Error()), nil, nil
	}

	entry := d.AgentRegistry.Disconnect(input.Alias)
	if entry == nil {
		return toolError(fmt.Sprintf("agent %q not found in registry", input.Alias)), nil, nil
	}

	// Clean up associated state.
	d.A2AClientResolver.Evict(entry.URL)
	d.RateLimiter.Remove(input.Alias)
	d.HealthTracker.Reset(input.Alias)
	d.ContextStore.Delete(input.Alias)

	if d.HistoryBackend != nil {
		_ = d.HistoryBackend.Delete(ctx, input.Alias)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{
			Text: fmt.Sprintf("Disconnected agent %q", input.Alias),
		}},
	}, nil, nil
}
