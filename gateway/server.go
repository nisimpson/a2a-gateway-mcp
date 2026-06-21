package gateway

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nisimpson/a2a-gateway-mcp/health"
)

const (
	defaultServerName    = "a2a-gateway-mcp"
	defaultServerVersion = "0.1.0"
	defaultHTTPTimeout   = 30 * time.Second
	taskPollTimeout      = 60 * time.Second
	defaultStreamTimeout = 60 * time.Second
)

// HealthCheckOptions configures the health tracking subsystem.
type HealthCheckOptions struct {
	// FailureThreshold is the number of consecutive failures before marking
	// an agent unhealthy. Default: 3. Zero disables tracking.
	FailureThreshold int

	// PingStrategy is the strategy used for on-demand liveness checks via
	// the ping_agent tool. If nil, DefaultPingStrategy (HTTP GET to agent
	// card endpoint) is used.
	PingStrategy PingStrategy
}

// HistoryOptions configures the history subsystem. Pass to WithHistory().
// Zero-value fields use sensible defaults.
type HistoryOptions struct {
	// Depth is the maximum number of entries per agent (default: 50).
	// Set to 0 to disable history recording entirely.
	Depth int

	// MaxEntryLength is the maximum character length for message/response
	// summaries (default: 1000). Longer text is truncated with "…".
	MaxEntryLength int

	// Backend is the storage implementation (default: in-memory).
	// Must be safe for concurrent use.
	Backend HistoryBackend
}

// Option configures the Gateway server at initialization time.
type Option func(*serverConfig)

// serverConfig holds immutable configuration set at initialization.
type serverConfig struct {
	httpClient     *http.Client
	name           string
	version        string
	pollTimeout    time.Duration
	streamTimeout  time.Duration
	rateLimitRPS   float64
	rateLimitBurst int

	// Health check configuration
	healthCheckConfigured  bool // true when WithHealthCheck was called
	healthFailureThreshold int
	healthPingStrategy     PingStrategy

	// History configuration
	historyConfigured bool // true when WithHistory was called
	historyDepth      int
	historyMaxEntry   int
	historyBackend    HistoryBackend
}

// WithHTTPClient sets a custom http.Client for all outbound A2A requests.
func WithHTTPClient(c *http.Client) Option {
	return func(cfg *serverConfig) {
		cfg.httpClient = c
	}
}

// WithName sets the MCP server name (default: "a2a-gateway-mcp").
func WithName(name string) Option {
	return func(cfg *serverConfig) {
		cfg.name = name
	}
}

// WithVersion sets the MCP server version (default: "0.1.0").
func WithVersion(version string) Option {
	return func(cfg *serverConfig) {
		cfg.version = version
	}
}

// WithPollTimeout sets the default timeout for polling non-terminal task states
// (default: 60s). Can be overridden per-request via poll_timeout_seconds.
func WithPollTimeout(d time.Duration) Option {
	return func(cfg *serverConfig) {
		cfg.pollTimeout = d
	}
}

// WithStreamTimeout sets the default timeout for SSE streaming responses
// (default: 60s). Can be overridden per-request via poll_timeout_seconds.
func WithStreamTimeout(d time.Duration) Option {
	return func(cfg *serverConfig) {
		cfg.streamTimeout = d
	}
}

// WithRateLimit sets the global default rate limit applied to all agents that
// do not specify a per-agent override at connect time. requestsPerSecond
// controls the sustained request rate and burst controls the maximum number of
// requests allowed in a single burst. Zero values for either parameter disable
// rate limiting (all requests are allowed).
func WithRateLimit(requestsPerSecond float64, burst int) Option {
	return func(cfg *serverConfig) {
		cfg.rateLimitRPS = requestsPerSecond
		cfg.rateLimitBurst = burst
	}
}

// WithHistory configures the interaction history subsystem.
// Zero-value fields in opts use defaults: depth=50, maxEntryLength=1000,
// backend=MemoryBackend. To disable history entirely, set Depth to 0.
func WithHistory(opts HistoryOptions) Option {
	return func(cfg *serverConfig) {
		cfg.historyConfigured = true
		cfg.historyDepth = opts.Depth
		cfg.historyMaxEntry = opts.MaxEntryLength
		cfg.historyBackend = opts.Backend
	}
}

// WithHealthCheck configures health tracking. A threshold of 0 or negative
// disables health tracking entirely.
func WithHealthCheck(opts HealthCheckOptions) Option {
	return func(cfg *serverConfig) {
		cfg.healthCheckConfigured = true
		cfg.healthFailureThreshold = opts.FailureThreshold
		cfg.healthPingStrategy = opts.PingStrategy
	}
}

// Server is the A2A Gateway MCP server. It wraps an mcp.Server and manages
// the agent registry and context store.
type Server struct {
	mcpServer     *mcp.Server
	registry      *AgentRegistry
	contextStore  *ContextStore
	httpClient    *http.Client
	clients       *clientResolver
	pollTimeout   time.Duration
	streamTimeout time.Duration

	// Rate limiting — Requirement: RLIM-3.1
	rateLimiters     *RateLimiterRegistry
	defaultRateLimit *RateLimitConfig // nil means no global default (unlimited)

	// Requirement: CAC-1.9 — global caller card state
	callerCard    *CallerCard // nil when no card is registered
	callerCardKey string      // metadata key; empty means use default
	callerCardMu  sync.RWMutex

	// History subsystem — Requirements: 1.3, 6.3
	historyBackend HistoryBackend
	historyEnabled bool
	historyDepth   int
	maxEntryLength int

	// Health tracking — Requirements: HLTH-4.1, HLTH-4.2, HLTH-4.3, HLTH-4.4, HLTH-4.5
	healthTracker *health.HealthTracker
	pingStrategy  PingStrategy
}

// NewServer creates a new gateway server with the given options.
func NewServer(opts ...Option) *Server {
	cfg := &serverConfig{
		httpClient:    &http.Client{Timeout: defaultHTTPTimeout},
		name:          defaultServerName,
		version:       defaultServerVersion,
		pollTimeout:   taskPollTimeout,
		streamTimeout: defaultStreamTimeout,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    cfg.name,
		Version: cfg.version,
	}, nil)

	s := &Server{
		mcpServer:     mcpServer,
		registry:      NewAgentRegistry(),
		contextStore:  NewContextStore(),
		httpClient:    cfg.httpClient,
		pollTimeout:   cfg.pollTimeout,
		streamTimeout: cfg.streamTimeout,
		rateLimiters:  NewRateLimiterRegistry(),
	}

	// Set global default rate limit if both RPS and burst are positive.
	if cfg.rateLimitRPS > 0 && cfg.rateLimitBurst > 0 {
		s.defaultRateLimit = &RateLimitConfig{
			RequestsPerSecond: cfg.rateLimitRPS,
			Burst:             cfg.rateLimitBurst,
		}
	}

	// Configure history subsystem.
	if cfg.historyConfigured {
		// User explicitly called WithHistory.
		if cfg.historyDepth == 0 {
			// Depth=0 disables history entirely.
			s.historyEnabled = false
			s.historyDepth = 0
			s.maxEntryLength = 0
			s.historyBackend = nil
		} else {
			depth := cfg.historyDepth
			maxEntry := cfg.historyMaxEntry
			if maxEntry <= 0 {
				maxEntry = 1000
			}
			backend := cfg.historyBackend
			if backend == nil {
				backend = NewMemoryBackend(depth)
			}
			s.historyEnabled = true
			s.historyDepth = depth
			s.maxEntryLength = maxEntry
			s.historyBackend = backend
		}
	} else {
		// No WithHistory call — apply defaults.
		s.historyEnabled = true
		s.historyDepth = 50
		s.maxEntryLength = 1000
		s.historyBackend = NewMemoryBackend(50)
	}

	s.clients = newClientResolver(s.registry, s.httpClient)

	// Configure health tracking subsystem — Requirements: HLTH-4.1, HLTH-4.3, HLTH-4.4
	threshold := 3
	if cfg.healthCheckConfigured {
		threshold = max(cfg.healthFailureThreshold, 0)
	}
	s.healthTracker = health.NewHealthTracker(threshold)

	// Configure ping strategy (default: HTTP GET to agent card endpoint).
	// Uses the server's existing HTTP client — no new client allocation.
	if cfg.healthPingStrategy != nil {
		s.pingStrategy = cfg.healthPingStrategy
	} else {
		s.pingStrategy = NewDefaultPingStrategy(s.httpClient)
	}

	s.registerToolsV2()

	return s
}

// Run starts the MCP server on the stdio transport, blocking until the
// client disconnects or an error occurs.
func (s *Server) Run(ctx context.Context) error {
	transport := &mcp.StdioTransport{}
	defer func() {
		if s.historyEnabled && s.historyBackend != nil {
			_ = s.historyBackend.Close(ctx)
		}
	}()
	return s.mcpServer.Run(ctx, transport)
}

// MCPServer returns the underlying mcp.Server for advanced use cases.
func (s *Server) MCPServer() *mcp.Server {
	return s.mcpServer
}

// effectivePollTimeout returns the poll timeout to use for a request.
// Per-request PollTimeoutSeconds takes precedence over the server default.
// A negative value means no timeout (wait indefinitely).
func (s *Server) effectivePollTimeout(requestSeconds *int) time.Duration {
	if requestSeconds != nil {
		if *requestSeconds < 0 {
			return 0 // sentinel: no timeout
		}
		if *requestSeconds > 0 {
			return time.Duration(*requestSeconds) * time.Second
		}
	}
	return s.pollTimeout
}

// effectiveStreamTimeout returns the stream timeout to use for a request.
// Per-request PollTimeoutSeconds takes precedence over the server default.
// A negative value means no timeout (wait indefinitely).
func (s *Server) effectiveStreamTimeout(requestSeconds *int) time.Duration {
	if requestSeconds != nil {
		if *requestSeconds < 0 {
			return 0 // sentinel: no timeout
		}
		if *requestSeconds > 0 {
			return time.Duration(*requestSeconds) * time.Second
		}
	}
	return s.streamTimeout
}
