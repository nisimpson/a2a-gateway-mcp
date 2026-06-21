package gateway

import (
	"context"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/nisimpson/a2a-gateway-mcp/internal/history"
	"github.com/nisimpson/a2a-gateway-mcp/internal/registry"
	"github.com/nisimpson/a2a-gateway-mcp/internal/tool"
	"github.com/nisimpson/a2a-gateway-mcp/internal/validate"
)

func (s *Server) registerToolsV2() {
	env := &tool.Env{
		AgentRegistry:          s.registryAdapter(),
		HealthTracker:          s.healthTracker,
		RateLimiter:            s.rateLimiters,
		HistoryBackend:         s.historyBackendAdapter(),
		A2AClientResolver:      s.clientResolverAdapter(),
		CallerCardInjector:     s.callerCardInjectorAdapter(),
		CallerCardStore:        s.callerCardStoreAdapter(),
		ContextStore:           s.contextStore,
		HistoryRecorder:        s.historyRecorderAdapter(),
		AgentCardFetcher:       s.agentCardFetcherAdapter(),
		HTTPDoer:               s.httpClient,
		PingStrategy:           s.pingStrategyAdapter(),
		EffectivePollTimeout:   s.effectivePollTimeout,
		EffectiveStreamTimeout: s.effectiveStreamTimeout,
		DefaultRateLimit:       s.toolDefaultRateLimit(),
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
// Wraps the gateway's *AgentRegistry and converts its types to
// *registry.RegisteredAgent for the tool interface.

func (s *Server) registryAdapter() *agentRegistryAdapter {
	return &agentRegistryAdapter{registry: s.registry}
}

type agentRegistryAdapter struct {
	registry *AgentRegistry
}

func (a *agentRegistryAdapter) Lookup(alias string) *registry.RegisteredAgent {
	entry := a.registry.Lookup(alias)
	if entry == nil {
		return nil
	}
	return toRegisteredAgent(entry)
}

func (a *agentRegistryAdapter) List() []*registry.RegisteredAgent {
	entries := a.registry.List()
	out := make([]*registry.RegisteredAgent, len(entries))
	for i, e := range entries {
		out[i] = toRegisteredAgent(e)
	}
	return out
}

func (a *agentRegistryAdapter) Connect(alias, url string, headers map[string]string, pingEndpoint string) bool {
	return a.registry.Connect(alias, url, headers, pingEndpoint)
}

func (a *agentRegistryAdapter) Disconnect(alias string) *registry.RegisteredAgent {
	entry := a.registry.Disconnect(alias)
	if entry == nil {
		return nil
	}
	return toRegisteredAgent(entry)
}

func (a *agentRegistryAdapter) SetCard(alias string, card *a2a.AgentCard) bool {
	return a.registry.SetCard(alias, card)
}

func (a *agentRegistryAdapter) ResolveAgent(identifier string) (*tool.ResolveResult, error) {
	if entry := a.registry.Lookup(identifier); entry != nil {
		return &tool.ResolveResult{
			URL:     entry.URL,
			Headers: entry.Headers,
			IsAlias: true,
			Alias:   identifier,
		}, nil
	}

	if err := validate.URL(identifier); err == nil {
		return &tool.ResolveResult{
			URL:     identifier,
			Headers: nil,
			IsAlias: false,
		}, nil
	}

	return nil, fmt.Errorf("agent alias not registered and identifier is not a valid URL")
}

func (a *agentRegistryAdapter) SupportsStreaming(resolved *tool.ResolveResult) bool {
	if !resolved.IsAlias {
		return false
	}
	entry := a.registry.Lookup(resolved.Alias)
	if entry == nil || entry.Card == nil {
		return false
	}
	return entry.Card.Capabilities.Streaming
}

// toRegisteredAgent converts a gateway AgentEntry to a registry.RegisteredAgent.
func toRegisteredAgent(e *AgentEntry) *registry.RegisteredAgent {
	return &registry.RegisteredAgent{
		Alias:        e.Alias,
		URL:          e.URL,
		Headers:      e.Headers,
		Card:         e.Card,
		PingEndpoint: e.PingEndpoint,
	}
}

// --- A2AClientResolver adapter ---

func (s *Server) clientResolverAdapter() *a2aClientResolverAdapter {
	return &a2aClientResolverAdapter{resolver: s.clients}
}

type a2aClientResolverAdapter struct {
	resolver *clientResolver
}

func (a *a2aClientResolverAdapter) Evict(url string) {
	a.resolver.Evict(url)
}

func (a *a2aClientResolverAdapter) Resolve(ctx context.Context, resolved *tool.ResolveResult) (*a2aclient.Client, error) {
	// Convert tool.ResolveResult to gateway.ResolveResult for the underlying resolver.
	gwResolved := &ResolveResult{
		URL:     resolved.URL,
		Headers: resolved.Headers,
		IsAlias: resolved.IsAlias,
		Alias:   resolved.Alias,
	}
	return a.resolver.Resolve(ctx, gwResolved)
}

// --- PingStrategy adapter ---

func (s *Server) pingStrategyAdapter() *pingStrategyAdapter {
	return &pingStrategyAdapter{strategy: s.pingStrategy}
}

type pingStrategyAdapter struct {
	strategy PingStrategy
}

func (a *pingStrategyAdapter) Ping(ctx context.Context, target tool.PingTarget) tool.PingResult {
	gwTarget := PingTarget{
		Alias:        target.Alias,
		URL:          target.URL,
		Headers:      target.Headers,
		PingEndpoint: target.PingEndpoint,
	}
	gwResult := a.strategy.Ping(ctx, gwTarget)
	return tool.PingResult{
		Reachable:    gwResult.Reachable,
		ResponseTime: gwResult.ResponseTime,
		Err:          gwResult.Err,
	}
}

// --- CallerCardInjector adapter ---

func (s *Server) callerCardInjectorAdapter() *callerCardInjectorAdapter {
	return &callerCardInjectorAdapter{server: s}
}

type callerCardInjectorAdapter struct {
	server *Server
}

func (a *callerCardInjectorAdapter) InjectCallerCard(metadata map[string]any) map[string]any {
	return a.server.injectCallerCard(metadata)
}

// --- CallerCardStore adapter ---

func (s *Server) callerCardStoreAdapter() *callerCardStoreAdapter {
	return &callerCardStoreAdapter{server: s}
}

type callerCardStoreAdapter struct {
	server *Server
}

func (a *callerCardStoreAdapter) Set(card *tool.CallerCard, metadataKey string) {
	a.server.callerCardMu.Lock()
	defer a.server.callerCardMu.Unlock()

	a.server.callerCard = &CallerCard{
		Name:        card.Name,
		Description: card.Description,
		URL:         card.URL,
	}
	if metadataKey != "" {
		a.server.callerCardKey = metadataKey
	} else {
		a.server.callerCardKey = defaultCallerCardKey
	}
}

func (a *callerCardStoreAdapter) Get() *tool.CallerCard {
	a.server.callerCardMu.RLock()
	defer a.server.callerCardMu.RUnlock()

	if a.server.callerCard == nil {
		return nil
	}
	return &tool.CallerCard{
		Name:        a.server.callerCard.Name,
		Description: a.server.callerCard.Description,
		URL:         a.server.callerCard.URL,
	}
}

func (a *callerCardStoreAdapter) Remove() bool {
	a.server.callerCardMu.Lock()
	defer a.server.callerCardMu.Unlock()

	if a.server.callerCard == nil {
		return false
	}
	a.server.callerCard = nil
	a.server.callerCardKey = ""
	return true
}

// --- HistoryBackend adapter ---
// Wraps gateway.HistoryBackend to satisfy tool.HistoryBackend (uses history.Entry).

func (s *Server) historyBackendAdapter() tool.HistoryBackend {
	if s.historyBackend == nil {
		return nil
	}
	return &historyBackendAdapter{backend: s.historyBackend}
}

type historyBackendAdapter struct {
	backend HistoryBackend
}

func (a *historyBackendAdapter) List(ctx context.Context, alias string) ([]history.Entry, error) {
	entries, err := a.backend.List(ctx, alias)
	if err != nil {
		return nil, err
	}
	out := make([]history.Entry, len(entries))
	for i, e := range entries {
		out[i] = history.Entry{
			Timestamp:   e.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
			SentMessage: e.SentMsg,
			Response:    e.Response,
			ContextID:   e.ContextID,
			TaskID:      e.TaskID,
			IsError:     e.IsError,
		}
	}
	return out, nil
}

func (a *historyBackendAdapter) Clear(ctx context.Context, alias string) error {
	return a.backend.Clear(ctx, alias)
}

func (a *historyBackendAdapter) Delete(ctx context.Context, alias string) error {
	return a.backend.Delete(ctx, alias)
}

// --- HistoryRecorder adapter ---
// Wraps gateway.HistoryBackend into a history.Recorder that satisfies tool.HistoryRecorder.

func (s *Server) historyRecorderAdapter() *historyRecorderAdapter {
	return &historyRecorderAdapter{server: s}
}

type historyRecorderAdapter struct {
	server *Server
}

func (a *historyRecorderAdapter) Record(ctx context.Context, input history.RecordInput) {
	a.server.recordHistory(ctx, input.Alias, input.Sent, input.Response, input.ContextID, input.TaskID, input.IsError)
}

// --- AgentCardFetcher adapter ---

func (s *Server) agentCardFetcherAdapter() *agentCardFetcherAdapter {
	return &agentCardFetcherAdapter{server: s}
}

type agentCardFetcherAdapter struct {
	server *Server
}

func (a *agentCardFetcherAdapter) FetchAgentCard(ctx context.Context, agentURL string, headers map[string]string) *a2a.AgentCard {
	return a.server.fetchAgentCard(ctx, agentURL, headers)
}
