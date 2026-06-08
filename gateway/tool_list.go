package gateway

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// listAgentEntry is the JSON representation of an agent in the list response.
type listAgentEntry struct {
	Alias               string `json:"alias"`
	URL                 string `json:"url"`
	RateLimit           string `json:"rate_limit"`
	Health              string `json:"health"`
	ConsecutiveFailures *int   `json:"consecutive_failures,omitempty"`
}

// handleListAgents returns a JSON array of all currently connected agents
// with their aliases and URLs, sorted by alias in ascending order.
func (s *Server) handleListAgents(_ context.Context, _ *mcp.CallToolRequest, _ ListAgentsInput) (*mcp.CallToolResult, any, error) {
	entries := s.registry.List()

	result := make([]listAgentEntry, len(entries))
	for i, entry := range entries {
		var rateLimit string
		rps, burst, exists := s.rateLimiters.Get(entry.Alias)
		if exists {
			rateLimit = fmt.Sprintf("%.2f rps, burst %d", rps, burst)
		} else {
			rateLimit = "unlimited"
		}
		healthState := s.healthTracker.Get(entry.Alias)
		var consecutiveFailures *int
		if healthState.Status == HealthStatusUnhealthy {
			consecutiveFailures = &healthState.Failures
		}

		result[i] = listAgentEntry{
			Alias:               entry.Alias,
			URL:                 entry.URL,
			RateLimit:           rateLimit,
			Health:              string(healthState.Status),
			ConsecutiveFailures: consecutiveFailures,
		}
	}

	data, err := json.Marshal(result)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "failed to serialize agent list: " + err.Error()}},
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}
