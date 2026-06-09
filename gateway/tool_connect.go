package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// handleConnectAgent registers a remote A2A agent with a friendly alias.
func (s *Server) handleConnectAgent(ctx context.Context, _ *mcp.CallToolRequest, input ConnectAgentInput) (*mcp.CallToolResult, any, error) {
	// Validate alias format.
	if err := ValidateAlias(input.Alias); err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}

	// Validate URL scheme.
	if err := ValidateURL(input.AgentURL); err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}

	// Validate headers count.
	if err := ValidateHeaders(input.Headers); err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}

	// Validate ping_endpoint if provided.
	if input.PingEndpoint != nil {
		if err := ValidatePingEndpoint(*input.PingEndpoint); err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, nil, nil
		}
	}

	// Validate rate limit parameters.
	if (input.RateLimitRPS != nil) != (input.RateLimitBurst != nil) {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "rate_limit_rps and rate_limit_burst must both be provided together"}},
		}, nil, nil
	}
	if input.RateLimitRPS != nil && *input.RateLimitRPS < 0 {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "rate_limit_rps must be non-negative"}},
		}, nil, nil
	}
	if input.RateLimitBurst != nil && *input.RateLimitBurst < 0 {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "rate_limit_burst must be non-negative"}},
		}, nil, nil
	}

	// Check if alias already exists; if so, evict cached client and clear context if URL changed.
	existing := s.registry.Lookup(input.Alias)
	if existing != nil {
		s.clients.Evict(existing.URL)
		if existing.URL != input.AgentURL {
			s.contextStore.Delete(input.Alias)
		}
	}

	// Add or update the registry entry.
	pingEndpoint := ""
	if input.PingEndpoint != nil {
		pingEndpoint = *input.PingEndpoint
	}
	s.registry.Connect(input.Alias, input.AgentURL, input.Headers, pingEndpoint)

	// Create or replace the rate limiter for the agent.
	if input.RateLimitRPS != nil && input.RateLimitBurst != nil {
		// Per-agent rate limit specified.
		cfg := &RateLimitConfig{RequestsPerSecond: *input.RateLimitRPS, Burst: *input.RateLimitBurst}
		if cfg.IsDisabled() {
			// Explicitly disabled (zero RPS or burst).
			s.rateLimiters.Remove(input.Alias)
		} else {
			s.rateLimiters.Set(input.Alias, cfg.RequestsPerSecond, cfg.Burst)
		}
	} else if s.defaultRateLimit != nil && !s.defaultRateLimit.IsDisabled() {
		// No per-agent config; apply global default.
		s.rateLimiters.Set(input.Alias, s.defaultRateLimit.RequestsPerSecond, s.defaultRateLimit.Burst)
	}

	// Attempt to fetch the AgentCard (best-effort; failure does not fail connect).
	card := s.fetchAgentCard(ctx, input.AgentURL, input.Headers)
	if card != nil {
		s.registry.SetCard(input.Alias, card)
	}

	// Initialize health state to (unknown, 0) for this agent.
	s.healthTracker.Reset(input.Alias)

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{
			Text: fmt.Sprintf("Connected agent %q at %s", input.Alias, input.AgentURL),
		}},
	}, nil, nil
}

// fetchAgentCard attempts to fetch an AgentCard from <agentURL>/.well-known/agent-card.json.
// Returns nil if the fetch fails for any reason (network error, non-200, invalid JSON).
func (s *Server) fetchAgentCard(ctx context.Context, agentURL string, headers map[string]string) *a2a.AgentCard {
	// Build the agent card URL.
	cardURL := strings.TrimRight(agentURL, "/") + "/.well-known/agent-card.json"

	// Use an HTTP client with the agent's headers applied.
	client := s.httpClient
	if len(headers) > 0 {
		entry := &AgentEntry{Headers: headers}
		client = httpClientForAgent(s.httpClient, entry)
	}

	// Create the request.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cardURL, nil)
	if err != nil {
		return nil
	}

	// Execute the request.
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	// Non-200 → leave card nil.
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	// Read body.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	// Parse into AgentCard.
	var card a2a.AgentCard
	if err := json.Unmarshal(body, &card); err != nil {
		return nil
	}

	return &card
}
