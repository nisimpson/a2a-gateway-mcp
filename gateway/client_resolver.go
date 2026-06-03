package gateway

import (
	"context"
	"net/http"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
)

// clientResolver creates and caches a2aclient.Client instances per agent.
// It inspects the agent's stored AgentCard to determine which transport
// to use. When no card is available, it defaults to JSON-RPC.
type clientResolver struct {
	mu         sync.RWMutex
	clients    map[string]*a2aclient.Client // key: agent URL
	registry   *AgentRegistry
	httpClient *http.Client
}

// newClientResolver creates a new clientResolver with the given registry
// and base HTTP client.
func newClientResolver(registry *AgentRegistry, httpClient *http.Client) *clientResolver {
	return &clientResolver{
		clients:    make(map[string]*a2aclient.Client),
		registry:   registry,
		httpClient: httpClient,
	}
}

// Resolve returns an a2aclient.Client for the given resolved agent.
// It uses a cached client if available, otherwise creates one from the
// agent's stored AgentCard. Falls back to JSON-RPC with the agent's URL
// when no card is available.
func (r *clientResolver) Resolve(ctx context.Context, resolved *ResolveResult) (*a2aclient.Client, error) {
	// Check cache with read lock.
	r.mu.RLock()
	if client, ok := r.clients[resolved.URL]; ok {
		r.mu.RUnlock()
		return client, nil
	}
	r.mu.RUnlock()

	// Upgrade to write lock for creation.
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock.
	if client, ok := r.clients[resolved.URL]; ok {
		return client, nil
	}

	// Build an HTTP client with the agent's custom headers applied.
	agentHTTPClient := r.httpClientForResolved(resolved)

	// Attempt to get the AgentCard if this is an alias-based resolution.
	var card *a2a.AgentCard
	if resolved.IsAlias {
		card = r.findCard(resolved)
	}

	client, err := r.createClient(ctx, resolved, card, agentHTTPClient)
	if err != nil {
		return nil, err
	}

	// Cache the client by URL.
	r.clients[resolved.URL] = client
	return client, nil
}

// Evict removes the cached client for the given URL, forcing re-creation
// on the next Resolve call.
func (r *clientResolver) Evict(url string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, url)
}

// createClient creates an a2aclient.Client using the agent's card or
// falling back to a JSON-RPC endpoint at the agent's URL.
func (r *clientResolver) createClient(ctx context.Context, resolved *ResolveResult, card *a2a.AgentCard, httpClient *http.Client) (*a2aclient.Client, error) {
	if card != nil && len(card.SupportedInterfaces) > 0 {
		// Determine transport option based on the card's primary interface protocol.
		var opts []a2aclient.FactoryOption
		switch card.SupportedInterfaces[0].ProtocolBinding {
		case a2a.TransportProtocolJSONRPC:
			opts = append(opts, a2aclient.WithJSONRPCTransport(httpClient))
		case a2a.TransportProtocolHTTPJSON:
			opts = append(opts, a2aclient.WithRESTTransport(httpClient))
		default:
			// Unknown protocol binding — fall through to JSON-RPC default.
			opts = append(opts, a2aclient.WithJSONRPCTransport(httpClient))
		}

		client, err := a2aclient.NewFromCard(ctx, card, opts...)
		if err == nil {
			return client, nil
		}
		// Fall through to JSON-RPC default if card-based creation fails.
	}

	// No card or card creation failed — default to JSON-RPC.
	endpoints := []*a2a.AgentInterface{
		a2a.NewAgentInterface(resolved.URL, a2a.TransportProtocolJSONRPC),
	}
	return a2aclient.NewFromEndpoints(ctx, endpoints, a2aclient.WithJSONRPCTransport(httpClient))
}

// findCard looks up the AgentCard for an alias-based resolution by
// searching all registry entries for one matching the resolved URL.
func (r *clientResolver) findCard(resolved *ResolveResult) *a2a.AgentCard {
	entries := r.registry.List()
	for _, entry := range entries {
		if entry.URL == resolved.URL && entry.Card != nil {
			return entry.Card
		}
	}
	return nil
}

// httpClientForResolved returns an HTTP client with the agent's custom
// headers injected via a headerRoundTripper.
func (r *clientResolver) httpClientForResolved(resolved *ResolveResult) *http.Client {
	if len(resolved.Headers) == 0 {
		return r.httpClient
	}
	entry := &AgentEntry{Headers: resolved.Headers}
	return httpClientForAgent(r.httpClient, entry)
}
