package tool

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ListAgentsInput is the input schema for the list_agents tool (empty).
type ListAgentsInput struct{}

// ListAgentsOutput is the output schema for the list_agents tool.
type ListAgentsOutput struct {
	Agents []ListAgentEntry `json:"agents" jsonschema:"list of registered agents with health and rate limit info"`
}

// ListAgentEntry describes a single agent in the list_agents output.
type ListAgentEntry struct {
	Alias               string `json:"alias" jsonschema:"agent alias"`
	URL                 string `json:"url" jsonschema:"agent URL"`
	RateLimit           string `json:"rate_limit" jsonschema:"rate limit description (e.g. '1.00 rps, burst 5' or 'unlimited')"`
	Health              string `json:"health" jsonschema:"health status (healthy, unhealthy, unknown)"`
	ConsecutiveFailures *int   `json:"consecutive_failures,omitempty" jsonschema:"failure count (only present when unhealthy)"`
}

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
