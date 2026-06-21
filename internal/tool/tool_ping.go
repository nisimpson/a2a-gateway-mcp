package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nisimpson/a2a-gateway-mcp/internal/health"
)

// PingAgentInput is the input schema for the ping_agent tool.
type PingAgentInput struct {
	Alias string `json:"alias" jsonschema:"alias of the agent to ping (lowercase alphanumeric and hyphens, max 64 chars)"`
}

// PingAgentOutput is the structured output for ping_agent.
type PingAgentOutput struct {
	Reachable    bool   `json:"reachable"`
	Health       string `json:"health"`
	ResponseTime *int   `json:"response_time_ms,omitempty"`
}

const pingTimeout = 5 * time.Second

// PingAgentTool performs on-demand liveness checks for registered agents.
type PingAgentTool struct {
	AgentRegistry AgentRegistry
	HealthTracker HealthTracker
	PingStrategy  PingStrategy
}

func (p *PingAgentTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "ping_agent",
		Description: "Perform a liveness check on a registered agent to verify reachability",
	}
}

func (p *PingAgentTool) Handle(ctx context.Context, _ *mcp.CallToolRequest, input *PingAgentInput) (*mcp.CallToolResult, any, error) {
	if input.Alias == "" {
		return toolError("alias is required"), nil, nil
	}

	entry := p.AgentRegistry.Lookup(input.Alias)
	if entry == nil {
		return toolError(fmt.Sprintf("agent %q not found in registry", input.Alias)), nil, nil
	}

	// Execute ping with a fixed timeout.
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()

	target := PingTarget{
		Alias:        input.Alias,
		URL:          entry.URL,
		Headers:      copyHeaders(entry.Headers),
		PingEndpoint: entry.PingEndpoint,
	}

	result := p.PingStrategy.Ping(pingCtx, target)

	// Update health based on outcome.
	p.updateHealth(input.Alias, result)

	// Read health state after update.
	healthStatus := p.HealthTracker.GetStatus(input.Alias)

	// Build response.
	output := PingAgentOutput{
		Reachable: result.Reachable,
		Health:    healthStatus,
	}
	if result.Reachable {
		ms := int(result.ResponseTime.Milliseconds())
		output.ResponseTime = &ms
	}

	data, err := json.Marshal(output)
	if err != nil {
		return toolError(fmt.Sprintf("failed to marshal ping response: %v", err)), nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

// updateHealth classifies the ping outcome and records health state.
func (p *PingAgentTool) updateHealth(alias string, result PingResult) {
	if !p.HealthTracker.IsEnabled() {
		return
	}
	if result.Reachable {
		p.HealthTracker.RecordSuccess(alias)
		return
	}
	outcome := health.ClassifyError(result.Err)
	switch outcome {
	case health.OutcomeConnectionError:
		p.HealthTracker.RecordFailure(alias)
	case health.OutcomeContextCanceled:
		// Do not update health state for client-initiated cancellation.
	default:
		p.HealthTracker.RecordSuccess(alias)
	}
}

// copyHeaders returns a shallow copy of the headers map.
func copyHeaders(h map[string]string) map[string]string {
	if h == nil {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, v := range h {
		out[k] = v
	}
	return out
}
