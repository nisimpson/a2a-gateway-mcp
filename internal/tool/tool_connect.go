package tool

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nisimpson/a2a-gateway-mcp/internal/validate"
)

// ConnectAgentInput is the input schema for the connect_agent tool.
type ConnectAgentInput struct {
	Alias          string            `json:"alias" jsonschema:"short alias for the agent (lowercase alphanumeric and hyphens only, max 64 chars)"`
	AgentURL       string            `json:"agent_url" jsonschema:"HTTP or HTTPS URL of the A2A agent"`
	Headers        map[string]string `json:"headers,omitempty" jsonschema:"optional HTTP headers to include on all requests to this agent (max 20 entries)"`
	RateLimitRPS   *float64          `json:"rate_limit_rps,omitempty" jsonschema:"requests per second rate limit for this agent (must be provided with rate_limit_burst)"`
	RateLimitBurst *int              `json:"rate_limit_burst,omitempty" jsonschema:"burst capacity for this agent's rate limiter (must be provided with rate_limit_rps)"`
	PingEndpoint   *string           `json:"ping_endpoint,omitempty" jsonschema:"relative URL path for liveness checks (starts with /, max 256 chars)"`
}

// ConnectAgentTool registers a remote A2A agent with a friendly alias.
type ConnectAgentTool struct {
	AgentRegistry     AgentRegistry
	A2AClientResolver A2AClientResolver
	ContextStore      ContextStore
	RateLimiter       RateLimiter
	HealthTracker     HealthTracker
	CardFetcher       AgentCardFetcher
	DefaultRateLimit  *RateLimitConfig
}

func (c *ConnectAgentTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "connect_agent",
		Description: "Register a remote A2A agent with a friendly alias for subsequent operations",
	}
}

func (c *ConnectAgentTool) Handle(ctx context.Context, _ *mcp.CallToolRequest, input *ConnectAgentInput) (*mcp.CallToolResult, any, error) {
	if err := c.validateInput(input); err != nil {
		return toolError(err.Error()), nil, nil
	}

	// Check if alias already exists; evict cached client and clear context if URL changed.
	if existing := c.AgentRegistry.Lookup(input.Alias); existing != nil {
		c.A2AClientResolver.Evict(existing.URL)
		if existing.URL != input.AgentURL {
			c.ContextStore.Delete(input.Alias)
		}
	}

	// Register the agent.
	pingEndpoint := ""
	if input.PingEndpoint != nil {
		pingEndpoint = *input.PingEndpoint
	}
	c.AgentRegistry.Connect(input.Alias, input.AgentURL, input.Headers, pingEndpoint)

	// Configure rate limiting.
	c.configureRateLimit(input)

	// Best-effort agent card fetch.
	if card := c.CardFetcher.FetchAgentCard(ctx, input.AgentURL, input.Headers); card != nil {
		c.AgentRegistry.SetCard(input.Alias, card)
	}

	// Initialize health state.
	c.HealthTracker.Reset(input.Alias)

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{
			Text: fmt.Sprintf("Connected agent %q at %s", input.Alias, input.AgentURL),
		}},
	}, nil, nil
}

func (c *ConnectAgentTool) validateInput(input *ConnectAgentInput) error {
	if err := validate.ValidateAlias(input.Alias); err != nil {
		return err
	}
	if err := validate.URL(input.AgentURL); err != nil {
		return err
	}
	if err := validate.ValidateHeaders(input.Headers); err != nil {
		return err
	}
	if input.PingEndpoint != nil {
		if err := validate.ValidatePingEndpoint(*input.PingEndpoint); err != nil {
			return err
		}
	}
	if (input.RateLimitRPS != nil) != (input.RateLimitBurst != nil) {
		return fmt.Errorf("rate_limit_rps and rate_limit_burst must both be provided together")
	}
	if input.RateLimitRPS != nil && *input.RateLimitRPS < 0 {
		return fmt.Errorf("rate_limit_rps must be non-negative")
	}
	if input.RateLimitBurst != nil && *input.RateLimitBurst < 0 {
		return fmt.Errorf("rate_limit_burst must be non-negative")
	}
	return nil
}

func (c *ConnectAgentTool) configureRateLimit(input *ConnectAgentInput) {
	if input.RateLimitRPS != nil && input.RateLimitBurst != nil {
		cfg := &RateLimitConfig{RequestsPerSecond: *input.RateLimitRPS, Burst: *input.RateLimitBurst}
		if cfg.IsDisabled() {
			c.RateLimiter.Remove(input.Alias)
		} else {
			c.RateLimiter.Set(input.Alias, cfg.RequestsPerSecond, cfg.Burst)
		}
	} else if c.DefaultRateLimit != nil && !c.DefaultRateLimit.IsDisabled() {
		c.RateLimiter.Set(input.Alias, c.DefaultRateLimit.RequestsPerSecond, c.DefaultRateLimit.Burst)
	}
}
