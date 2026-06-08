package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// PingAgentInput is the input schema for the ping_agent tool.
type PingAgentInput struct {
	Alias string `json:"alias" jsonschema:"alias of the agent to ping (lowercase alphanumeric and hyphens, max 64 chars)"`
}

// pingResponse is the JSON response from ping_agent.
type pingResponse struct {
	Reachable    bool   `json:"reachable"`
	Health       string `json:"health"`
	ResponseTime *int   `json:"response_time_ms,omitempty"`
}

const pingTimeout = 5 * time.Second

// handlePingAgent performs an on-demand liveness check for a registered agent.
func (s *Server) handlePingAgent(ctx context.Context, _ *mcp.CallToolRequest, input PingAgentInput) (*mcp.CallToolResult, any, error) {
	// Validate alias format.
	if err := ValidateAlias(input.Alias); err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}

	// Lookup agent in registry.
	entry := s.registry.Lookup(input.Alias)
	if entry == nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("agent %q not found in registry", input.Alias)}},
		}, nil, nil
	}

	// Create a 5-second timeout context independent of the server HTTP timeout.
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()

	// Build PingTarget by copying alias, URL, headers (shallow copy), and PingEndpoint.
	headers := make(map[string]string, len(entry.Headers))
	for k, v := range entry.Headers {
		headers[k] = v
	}

	target := PingTarget{
		Alias:        entry.Alias,
		URL:          entry.URL,
		Headers:      headers,
		PingEndpoint: entry.PingEndpoint,
	}

	// Execute ping via the configured strategy.
	result := s.pingStrategy.Ping(pingCtx, target)

	// Classify outcome and update health tracker (skip if tracking disabled).
	if s.healthTracker.IsEnabled() {
		if result.Reachable {
			s.healthTracker.RecordSuccess(input.Alias)
		} else {
			outcome := ClassifyError(result.Err)
			switch outcome {
			case OutcomeConnectionError:
				s.healthTracker.RecordFailure(input.Alias)
			case OutcomeContextCanceled:
				// Do not update health state for client-initiated cancellation.
			default:
				// OutcomeSuccess should not occur when !result.Reachable, but handle gracefully.
				s.healthTracker.RecordSuccess(input.Alias)
			}
		}
	}

	// Get health status AFTER the update.
	state := s.healthTracker.Get(input.Alias)

	// Build the response.
	resp := pingResponse{
		Reachable: result.Reachable,
		Health:    string(state.Status),
	}

	// Include response_time_ms only when reachable.
	if result.Reachable {
		ms := int(result.ResponseTime.Milliseconds())
		resp.ResponseTime = &ms
	}

	// Marshal to JSON.
	data, err := json.Marshal(resp)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to marshal ping response: %v", err)}},
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}
