package tool

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ListAgentsTool lists all currently connected agents.
type ListAgentsTool struct {
	AgentRegistry AgentRegistry
	RateLimiter   RateLimiter
	HealthTracker HealthTracker
}

// NewListAgentsTool creates a new ListAgentsTool initialized with the agent registry,
// rate limiter, and health tracker from the provided environment.
func NewListAgentsTool(env *Env) *ListAgentsTool {
	return &ListAgentsTool{
		AgentRegistry: env.AgentRegistry,
		RateLimiter:   env.RateLimiter,
		HealthTracker: env.HealthTracker,
	}
}

func (l *ListAgentsTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "list_agents",
		Description: "List all currently connected agents with their aliases and URLs",
	}
}

func (l *ListAgentsTool) Handle(_ context.Context, _ *mcp.CallToolRequest, _ *ListAgentsInput) (*mcp.CallToolResult, *ListAgentsOutput, error) {
	entries := l.AgentRegistry.List()

	result := make([]ListAgentEntry, len(entries))
	for i, entry := range entries {
		rateLimit := "unlimited"
		if rps, burst, exists := l.RateLimiter.Get(entry.Alias); exists {
			rateLimit = fmt.Sprintf("%.2f rps, burst %d", rps, burst)
		}

		healthStatus := l.HealthTracker.GetStatus(entry.Alias)
		var consecutiveFailures *int
		if failures, unhealthy := l.HealthTracker.GetFailures(entry.Alias); unhealthy {
			consecutiveFailures = &failures
		}

		result[i] = ListAgentEntry{
			Alias:               entry.Alias,
			URL:                 entry.URL,
			RateLimit:           rateLimit,
			Health:              healthStatus,
			ConsecutiveFailures: consecutiveFailures,
		}
	}

	return nil, &ListAgentsOutput{Agents: result}, nil
}
