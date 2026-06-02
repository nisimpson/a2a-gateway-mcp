package gateway

import (
	"context"
	"net/http"
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
	httpClient *http.Client
	name       string
	version    string
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

// Server is the A2A Gateway MCP server. It wraps an mcp.Server and manages
// the agent registry and context store.
type Server struct {
	mcpServer    *mcp.Server
	registry     *AgentRegistry
	contextStore *ContextStore
	httpClient   *http.Client
	clients      *clientResolver
}

// NewServer creates a new gateway server with the given options.
func NewServer(opts ...Option) *Server {
	cfg := &serverConfig{
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
		name:       defaultServerName,
		version:    defaultServerVersion,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    cfg.name,
		Version: cfg.version,
	}, nil)

	s := &Server{
		mcpServer:    mcpServer,
		registry:     NewAgentRegistry(),
		contextStore: NewContextStore(),
		httpClient:   cfg.httpClient,
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
