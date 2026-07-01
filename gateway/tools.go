package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/nisimpson/a2a-gateway-mcp/directory"
	"github.com/nisimpson/a2a-gateway-mcp/history"
	"github.com/nisimpson/a2a-gateway-mcp/internal/tool"
	"github.com/nisimpson/a2a-gateway-mcp/internal/validate"
	"github.com/nisimpson/a2a-gateway-mcp/registry"
)

// Compile-time interface check: MemoryInbox must satisfy tool.Inbox.
var _ tool.Inbox = (*registry.MemoryInbox)(nil)

func (s *Server) registerToolsV2() {
	// Build the directory querier if a self-hosted directory is configured.
	var dir tool.DirectoryQuerier
	if s.directory != nil {
		dir = &directoryAdapter{dir: s.directory}
	}

	env := &tool.Env{
		AgentRegistry:          s.registryAdapter(),
		HealthTracker:          s.healthTracker,
		RateLimiter:            s.rateLimiters,
		HistoryBackend:         s.historyBackendAdapter(),
		A2AClientResolver:      s.clients,
		CallerCardInjector:     s.callerCardStore,
		CallerCardStore:        s.callerCardStore,
		ContextStore:           s.contextStore,
		HistoryRecorder:        s.historyRecorderAdapter(),
		AgentCardFetcher:       s.agentCardFetcherAdapter(),
		HTTPDoer:               s.httpClient,
		PingStrategy:           s.pingStrategy,
		Inbox:                  s.inbox,
		EffectivePollTimeout:   s.effectivePollTimeout,
		EffectiveStreamTimeout: s.effectiveStreamTimeout,
		DefaultRateLimit:       s.toolDefaultRateLimit(),

		// Directory discovery — Requirements: 1.2, 2.2, 2.4, 3.5
		DefaultDirectoryURL:    s.defaultDirectoryURL,
		DefaultDirectoryURLErr: s.defaultDirectoryURLErr,
		Directory:              dir,
	}
	tool.RegisterAll(s.mcpServer, env)
}

func (s *Server) toolDefaultRateLimit() tool.RateLimitConfig {
	if s.defaultRateLimit == nil {
		return tool.RateLimitConfig{}
	}
	return tool.RateLimitConfig{
		RequestsPerSecond: s.defaultRateLimit.RequestsPerSecond,
		Burst:             s.defaultRateLimit.Burst,
	}
}

// --- AgentRegistry adapter ---
// Wraps *registry.AgentRegistry and adds ResolveAgent/SupportsStreaming
// which are not part of the registry package itself.

func (s *Server) registryAdapter() *agentRegistryAdapter {
	return &agentRegistryAdapter{registry: s.registry}
}

type agentRegistryAdapter struct {
	registry *registry.AgentRegistry
}

func (a *agentRegistryAdapter) Lookup(alias string) *registry.RegisteredAgent {
	return a.registry.Lookup(alias)
}

func (a *agentRegistryAdapter) List() []*registry.RegisteredAgent {
	return a.registry.List()
}

func (a *agentRegistryAdapter) Connect(alias, url string, headers map[string]string, pingEndpoint string) bool {
	return a.registry.Connect(alias, url, headers, pingEndpoint)
}

func (a *agentRegistryAdapter) Disconnect(alias string) *registry.RegisteredAgent {
	return a.registry.Disconnect(alias)
}

func (a *agentRegistryAdapter) SetCard(alias string, card *a2a.AgentCard) bool {
	return a.registry.SetCard(alias, card)
}

func (a *agentRegistryAdapter) ResolveAgent(identifier string) (*registry.ResolveResult, error) {
	if entry := a.registry.Lookup(identifier); entry != nil {
		return &registry.ResolveResult{
			URL:     entry.URL,
			Headers: entry.Headers,
			IsAlias: true,
			Alias:   identifier,
		}, nil
	}

	if err := validate.URL(identifier); err == nil {
		return &registry.ResolveResult{
			URL:     identifier,
			Headers: nil,
			IsAlias: false,
		}, nil
	}

	return nil, fmt.Errorf("agent alias not registered and identifier is not a valid URL")
}

func (a *agentRegistryAdapter) SupportsStreaming(resolved *registry.ResolveResult) bool {
	if !resolved.IsAlias {
		return false
	}
	entry := a.registry.Lookup(resolved.Alias)
	if entry == nil || entry.Card == nil {
		return false
	}
	return entry.Card.Capabilities.Streaming
}

// --- HistoryBackend adapter ---
// history.Backend satisfies tool.HistoryBackend directly (List, Clear, Delete).

func (s *Server) historyBackendAdapter() tool.HistoryBackend {
	if s.historyBackend == nil {
		return nil
	}
	return s.historyBackend
}

// --- HistoryRecorder adapter ---

func (s *Server) historyRecorderAdapter() *history.Recorder {
	return &history.Recorder{
		Backend:        s.historyBackend,
		Enabled:        s.historyEnabled,
		MaxEntryLength: s.maxEntryLength,
	}
}

// --- AgentCardFetcher adapter ---

func (s *Server) agentCardFetcherAdapter() *agentCardFetcherAdapter {
	return &agentCardFetcherAdapter{httpClient: s.httpClient}
}

type agentCardFetcherAdapter struct {
	httpClient *http.Client
}

func (a *agentCardFetcherAdapter) FetchAgentCard(ctx context.Context, agentURL string, headers map[string]string) *a2a.AgentCard {
	return registry.FetchAgentCard(ctx, a.httpClient, agentURL, headers)
}

// --- DirectoryQuerier adapter ---
// directoryAdapter wraps a *directory.Directory to satisfy tool.DirectoryQuerier.
// It constructs a synthetic HTTP request and captures the response via httptest
// to reuse the directory's existing ServeHTTP logic (filtering, pagination, help).

// Compile-time interface check: directoryAdapter must satisfy tool.DirectoryQuerier.
var _ tool.DirectoryQuerier = (*directoryAdapter)(nil)

type directoryAdapter struct {
	dir *directory.Directory
}

func (a *directoryAdapter) Query(ctx context.Context, filter string, limit int, cursor string, help bool) (*directory.QueryResult, error) {
	// Build query string from params.
	q := url.Values{}
	if help {
		q.Set("help", "true")
	} else {
		if filter != "" {
			q.Set("filter", filter)
		}
		if limit > 0 {
			q.Set("limit", strconv.Itoa(limit))
		}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
	}

	// Create synthetic request.
	reqURL := &url.URL{Path: "/", RawQuery: q.Encode()}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)

	// Capture response.
	rec := httptest.NewRecorder()
	a.dir.ServeHTTP(rec, req)

	// Handle error status codes.
	if rec.Code != http.StatusOK {
		return nil, fmt.Errorf("directory query failed: %s", rec.Body.String())
	}

	// Parse the QueryResult JSON from the response body.
	var result directory.QueryResult
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse directory response: %w", err)
	}
	return &result, nil
}
