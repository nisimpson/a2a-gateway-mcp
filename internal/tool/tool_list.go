package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ListAgentsInput is the input schema for the list_agents tool (empty).
type ListAgentsInput struct{}

// listAgentEntry is the JSON representation of an agent in the list response.
type listAgentEntry struct {
	Alias               string `json:"alias"`
	URL                 string `json:"url"`
	RateLimit           string `json:"rate_limit"`
	Health              string `json:"health"`
	ConsecutiveFailures *int   `json:"consecutive_failures,omitempty"`
}

// ListAgentsTool lists all currently connected agents.
type ListAgentsTool struct {
	AgentRegistry AgentRegistry
	RateLimiter   RateLimiter
	HealthTracker HealthTracker
}

func (l *ListAgentsTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "list_agents",
		Description: "List all currently connected agents with their aliases and URLs",
	}
}

func (l *ListAgentsTool) Handle(_ context.Context, _ *mcp.CallToolRequest, _ *ListAgentsInput) (*mcp.CallToolResult, any, error) {
	entries := l.AgentRegistry.List()

	result := make([]listAgentEntry, len(entries))
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

		result[i] = listAgentEntry{
			Alias:               entry.Alias,
			URL:                 entry.URL,
			RateLimit:           rateLimit,
			Health:              healthStatus,
			ConsecutiveFailures: consecutiveFailures,
		}
	}

	data, err := json.Marshal(result)
	if err != nil {
		return toolError("failed to serialize agent list: " + err.Error()), nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}
