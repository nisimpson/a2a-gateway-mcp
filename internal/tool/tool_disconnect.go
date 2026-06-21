package tool

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nisimpson/a2a-gateway-mcp/internal/validate"
)

// DisconnectAgentOutput is the output schema for the disconnect_agent tool.
type DisconnectAgentOutput struct {
	Message string `json:"message" jsonschema:"confirmation message indicating successful disconnection"`
}

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

// NewDisconnectAgentTool creates a new DisconnectAgentTool from the shared environment.
func NewDisconnectAgentTool(env *Env) *DisconnectAgentTool {
	return &DisconnectAgentTool{
		AgentRegistry:     env.AgentRegistry,
		A2AClientResolver: env.A2AClientResolver,
		ContextStore:      env.ContextStore,
		RateLimiter:       env.RateLimiter,
		HealthTracker:     env.HealthTracker,
		HistoryBackend:    env.HistoryBackend,
	}
}

func (d *DisconnectAgentTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "disconnect_agent",
		Description: "Remove a registered agent by alias from the gateway registry",
	}
}

func (d *DisconnectAgentTool) Handle(ctx context.Context, _ *mcp.CallToolRequest, input *DisconnectAgentInput) (*mcp.CallToolResult, *DisconnectAgentOutput, error) {
	if err := validate.Alias(input.Alias); err != nil {
		return nil, nil, err
	}

	entry := d.AgentRegistry.Disconnect(input.Alias)
	if entry == nil {
		return nil, nil, fmt.Errorf("agent %q not found in registry", input.Alias)
	}

	// Clean up associated state.
	d.A2AClientResolver.Evict(entry.URL)
	d.RateLimiter.Remove(input.Alias)
	d.HealthTracker.Reset(input.Alias)
	d.ContextStore.Delete(input.Alias)

	if d.HistoryBackend != nil {
		_ = d.HistoryBackend.Delete(ctx, input.Alias)
	}

	output := &DisconnectAgentOutput{
		Message: fmt.Sprintf("Disconnected agent %q", input.Alias),
	}
	return nil, output, nil
}
