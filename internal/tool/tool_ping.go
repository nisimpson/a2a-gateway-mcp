package tool

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nisimpson/a2a-gateway-mcp/internal/health"
)

// PingAgentInput is the input schema for the ping_agent tool.
type PingAgentInput struct {
	Alias string `json:"alias" jsonschema:"alias of the agent to ping (lowercase alphanumeric and hyphens, max 64 chars)"`
}

// PingAgentOutput is the output schema for the ping_agent tool.
type PingAgentOutput struct {
	Reachable    bool   `json:"reachable" jsonschema:"whether the agent responded to the ping"`
	Health       string `json:"health" jsonschema:"health status after ping (healthy, unhealthy, unknown)"`
	ResponseTime *int   `json:"response_time_ms,omitempty" jsonschema:"response time in milliseconds (only present when reachable)"`
}

const pingTimeout = 5 * time.Second

// PingAgentTool performs on-demand liveness checks for registered agents.
type PingAgentTool struct {
	AgentRegistry AgentRegistry
	HealthTracker HealthTracker
	PingStrategy  PingStrategy
}

// NewPingAgentTool creates a PingAgentTool wired with dependencies from the
// shared environment. It uses the registry to resolve agent aliases, the health
// tracker to record liveness state, and the ping strategy to execute the
// actual connectivity check.
func NewPingAgentTool(env *Env) *PingAgentTool {
	return &PingAgentTool{
		AgentRegistry: env.AgentRegistry,
		HealthTracker: env.HealthTracker,
		PingStrategy:  env.PingStrategy,
	}
}

func (p *PingAgentTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "ping_agent",
		Description: "Perform a liveness check on a registered agent to verify reachability",
	}
}

func (p *PingAgentTool) Handle(ctx context.Context, _ *mcp.CallToolRequest, input *PingAgentInput) (*mcp.CallToolResult, *PingAgentOutput, error) {
	if input.Alias == "" {
		return nil, nil, errors.New("alias is required")
	}

	entry := p.AgentRegistry.Lookup(input.Alias)
	if entry == nil {
		return nil, nil, fmt.Errorf("agent %q not found in registry", input.Alias)
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

	return nil, &output, nil
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
