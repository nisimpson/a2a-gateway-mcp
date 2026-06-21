package tool

import (
	"context"
	"net/http"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nisimpson/a2a-gateway-mcp/history"
	"github.com/nisimpson/a2a-gateway-mcp/registry"
)

type Env struct {
	AgentRegistry
	HealthTracker
	RateLimiter
	HistoryBackend
	A2AClientResolver
	CallerCardInjector
	CallerCardStore
	ContextStore
	HistoryRecorder
	AgentCardFetcher
	HTTPDoer
	PingStrategy
	EffectivePollTimeout   EffectiveTimeoutFunc
	EffectiveStreamTimeout EffectiveTimeoutFunc
	DefaultRateLimit       RateLimitConfig
}

// ToolDefinition defines the interface that all MCP tools must implement.
type ToolDefinition[T, U any] interface {
	Handle(ctx context.Context, req *mcp.CallToolRequest, in T) (res *mcp.CallToolResult, out U, err error)
	Tool() *mcp.Tool
}

// AddTool registers a tool definition with the MCP server.
func AddTool[T, U any](srv *mcp.Server, def ToolDefinition[T, U]) {
	mcp.AddTool(srv, def.Tool(), def.Handle)
}

// RegisterAll registers all gateway tools with the given MCP server.
func RegisterAll(srv *mcp.Server, env *Env) {
	AddTool(srv, NewSendMessageTool(env))
	AddTool(srv, NewBroadcastMessageTool(env))
	AddTool(srv, NewPingAgentTool(env))
	AddTool(srv, NewConnectAgentTool(env))
	AddTool(srv, NewDisconnectAgentTool(env))
	AddTool(srv, NewListAgentsTool(env))
	AddTool(srv, NewGetAgentCardTool(env))
	AddTool(srv, NewGetTaskTool(env))
	AddTool(srv, NewCancelTaskTool(env))
	AddTool(srv, NewGetHistoryTool(env))
	AddTool(srv, NewClearHistoryTool(env))
	AddTool(srv, NewCreateCallerCardTool(env))
	AddTool(srv, NewViewCallerCardTool(env))
	AddTool(srv, NewRemoveCallerCardTool(env))
	AddTool(srv, NewDiscoverAgentsTool(env))
}

// --- Shared types ---

// EffectiveTimeoutFunc computes the effective poll timeout given an optional
// per-request timeout override in seconds.
type EffectiveTimeoutFunc = func(requestSeconds *int) time.Duration

// ResolveResult contains the resolved agent information.
type ResolveResult struct {
	URL     string
	Headers map[string]string
	IsAlias bool   // true if resolved from registry, false if raw URL
	Alias   string // populated when IsAlias is true
}

// RateLimitConfig holds rate limit parameters.
type RateLimitConfig struct {
	RequestsPerSecond float64
	Burst             int
}

// IsDisabled returns true if the rate limit is effectively disabled (zero values).
func (c *RateLimitConfig) IsDisabled() bool {
	return c.RequestsPerSecond <= 0 || c.Burst <= 0
}

// --- Consolidated interfaces ---

// AgentRegistry manages registered agents and resolves identifiers.
type AgentRegistry interface {
	// Lookup retrieves the agent entry associated with the given alias.
	// Returns nil if no agent is registered under that alias.
	Lookup(alias string) *registry.RegisteredAgent
	// List returns all registered agent entries.
	List() []*registry.RegisteredAgent
	// Connect registers or updates an agent in the registry.
	Connect(alias, url string, headers map[string]string, pingEndpoint string) (updated bool)
	// Disconnect removes the agent entry and returns it, or nil if not found.
	Disconnect(alias string) *registry.RegisteredAgent
	// SetCard stores an agent card for the given alias.
	SetCard(alias string, card *a2a.AgentCard) bool
	// ResolveAgent resolves the given identifier to connection details for
	// an agent. The identifier may be a registered alias or a raw URL.
	ResolveAgent(identifier string) (*ResolveResult, error)
	// SupportsStreaming returns true if the resolved agent has a stored AgentCard
	// with Capabilities.Streaming set to true.
	SupportsStreaming(resolved *ResolveResult) bool
}

// HealthTracker monitors the health status of resolved agents by
// tracking consecutive failures.
type HealthTracker interface {
	// RecordFailure increments the consecutive failure count for the alias.
	RecordFailure(alias string)
	// RecordSuccess resets the failure count and sets status to healthy.
	RecordSuccess(alias string)
	// IsEnabled reports whether health tracking is active.
	IsEnabled() bool
	// IsHealthy reports whether the specified alias is currently healthy.
	IsHealthy(alias string) bool
	// GetStatus returns the health status string for the alias.
	GetStatus(alias string) string
	// Reset initializes or resets the health state for the alias.
	Reset(alias string)
	// GetFailures returns the consecutive failure count and whether the agent is unhealthy.
	GetFailures(alias string) (int, bool)
}

// RateLimiter manages per-agent rate limiting.
type RateLimiter interface {
	// Allow reports whether the alias is permitted to proceed.
	Allow(alias string) bool
	// CheckRateLimit returns a non-nil error if rate limited, nil if allowed.
	CheckRateLimit(alias string) error
	// Set configures the rate limit for an alias.
	Set(alias string, rps float64, burst int)
	// Remove deletes the rate limit for an alias.
	Remove(alias string)
	// Get returns the current rate limit config, or exists=false if none.
	Get(alias string) (rps float64, burst int, exists bool)
}

// HistoryBackend defines the storage interface for interaction history.
type HistoryBackend interface {
	// List retrieves all history entries associated with the given agent alias.
	List(ctx context.Context, alias string) ([]history.Entry, error)
	// Clear removes all history entries for the given agent alias but retains the alias key.
	Clear(ctx context.Context, alias string) error
	// Delete removes the agent alias and all associated history entries entirely.
	Delete(ctx context.Context, alias string) error
}

// A2AClientResolver manages the lifecycle of A2A clients for resolved agents.
type A2AClientResolver interface {
	// Evict removes the cached client associated with the given URL.
	Evict(url string)
	// Resolve returns an A2A client for the given resolved agent.
	Resolve(ctx context.Context, resolved *ResolveResult) (*a2aclient.Client, error)
}

// CallerCardInjector injects the caller's agent card into outgoing request metadata.
type CallerCardInjector interface {
	// InjectCallerCard merges the stored caller card into the given metadata map.
	InjectCallerCard(metadata map[string]any) map[string]any
}

// ContextStore manages the mapping between agent aliases and their context IDs.
type ContextStore interface {
	// Delete removes the stored context ID for the given alias.
	Delete(alias string)
	// Get retrieves the context ID associated with the given alias.
	Get(alias string) string
	// Set stores or updates the context ID for the given alias.
	Set(alias string, contextID string)
}

// HistoryRecorder persists interaction records for observability.
type HistoryRecorder interface {
	// Record persists the given interaction record.
	Record(ctx context.Context, input history.RecordInput)
}

// AgentCardFetcher fetches an agent card from a remote URL.
type AgentCardFetcher interface {
	FetchAgentCard(ctx context.Context, agentURL string, headers map[string]string) *a2a.AgentCard
}

// HTTPDoer is a narrow interface for executing HTTP requests.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// PingTarget holds the information needed to ping a single agent.
type PingTarget struct {
	Alias        string
	URL          string
	Headers      map[string]string
	PingEndpoint string
}

// PingResult holds the outcome of a ping operation.
type PingResult struct {
	Reachable    bool
	ResponseTime time.Duration
	Err          error
}

// PingStrategy defines how liveness checks are performed.
type PingStrategy interface {
	Ping(ctx context.Context, target PingTarget) PingResult
}

// CallerCardStore manages the stored caller card state.
type CallerCardStore interface {
	Set(card *CallerCard, metadataKey string)
	Get() *CallerCard
	Remove() bool // returns true if there was a card to remove
}
