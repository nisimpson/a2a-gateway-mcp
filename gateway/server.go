package gateway

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultServerName    = "a2a-gateway-mcp"
	defaultServerVersion = "0.1.0"
	defaultHTTPTimeout   = 30 * time.Second
)

// Option configures the Gateway server at initialization time.
type Option func(*serverConfig)

// serverConfig holds immutable configuration set at initialization.
type serverConfig struct {
	httpClient    *http.Client
	name          string
	version       string
	pollTimeout   time.Duration
	streamTimeout time.Duration
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

	// Requirement: CAC-1.9 — global caller card state
	callerCard    *CallerCard // nil when no card is registered
	callerCardKey string      // metadata key; empty means use default
	callerCardMu  sync.RWMutex
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
	}

	s.clients = newClientResolver(s.registry, s.httpClient)

	s.registerTools()

	return s
}

// Run starts the MCP server on the stdio transport, blocking until the
// client disconnects or an error occurs.
func (s *Server) Run(ctx context.Context) error {
	transport := &mcp.StdioTransport{}
	return s.mcpServer.Run(ctx, transport)
}

// MCPServer returns the underlying mcp.Server for advanced use cases.
func (s *Server) MCPServer() *mcp.Server {
	return s.mcpServer
}
