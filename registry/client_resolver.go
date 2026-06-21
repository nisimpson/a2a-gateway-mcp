package registry

import (
	"context"
	"net/http"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
)

// ClientResolver creates and caches a2aclient.Client instances per agent.
// It inspects the agent's stored AgentCard to determine which transport
// to use. When no card is available, it defaults to JSON-RPC.
type ClientResolver struct {
	mu         sync.RWMutex
	clients    map[string]*a2aclient.Client // key: agent URL
	registry   *AgentRegistry
	httpClient *http.Client
}

// NewClientResolver creates a new ClientResolver with the given registry
// and base HTTP client.
func NewClientResolver(reg *AgentRegistry, httpClient *http.Client) *ClientResolver {
	return &ClientResolver{
		clients:    make(map[string]*a2aclient.Client),
		registry:   reg,
		httpClient: httpClient,
	}
}

// Resolve returns an a2aclient.Client for the given resolved agent.
// It uses a cached client if available, otherwise creates one from the
// agent's stored AgentCard. Falls back to JSON-RPC with the agent's URL
// when no card is available.
func (r *ClientResolver) Resolve(ctx context.Context, resolved *ResolveResult) (*a2aclient.Client, error) {
	r.mu.RLock()
	if client, ok := r.clients[resolved.URL]; ok {
		r.mu.RUnlock()
		return client, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock.
	if client, ok := r.clients[resolved.URL]; ok {
		return client, nil
	}

	agentHTTPClient := r.httpClientForResolved(resolved)

	var card *a2a.AgentCard
	if resolved.IsAlias {
		card = r.findCard(resolved)
	}

	client, err := r.createClient(ctx, resolved, card, agentHTTPClient)
	if err != nil {
		return nil, err
	}

	r.clients[resolved.URL] = client
	return client, nil
}

// Evict removes the cached client for the given URL, forcing re-creation
// on the next Resolve call.
func (r *ClientResolver) Evict(url string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, url)
}

func (r *ClientResolver) createClient(ctx context.Context, resolved *ResolveResult, card *a2a.AgentCard, httpClient *http.Client) (*a2aclient.Client, error) {
	if card != nil && len(card.SupportedInterfaces) > 0 {
		var opts []a2aclient.FactoryOption
		switch card.SupportedInterfaces[0].ProtocolBinding {
		case a2a.TransportProtocolJSONRPC:
			opts = append(opts, a2aclient.WithJSONRPCTransport(httpClient))
		case a2a.TransportProtocolHTTPJSON:
			opts = append(opts, a2aclient.WithRESTTransport(httpClient))
		default:
			opts = append(opts, a2aclient.WithJSONRPCTransport(httpClient))
		}

		client, err := a2aclient.NewFromCard(ctx, card, opts...)
		if err == nil {
			return client, nil
		}
	}

	endpoints := []*a2a.AgentInterface{
		a2a.NewAgentInterface(resolved.URL, a2a.TransportProtocolJSONRPC),
	}
	return a2aclient.NewFromEndpoints(ctx, endpoints, a2aclient.WithJSONRPCTransport(httpClient))
}

func (r *ClientResolver) findCard(resolved *ResolveResult) *a2a.AgentCard {
	entries := r.registry.List()
	for _, entry := range entries {
		if entry.URL == resolved.URL && entry.Card != nil {
			return entry.Card
		}
	}
	return nil
}

func (r *ClientResolver) httpClientForResolved(resolved *ResolveResult) *http.Client {
	if len(resolved.Headers) == 0 {
		return r.httpClient
	}
	entry := &RegisteredAgent{Headers: resolved.Headers}
	return HTTPClientForAgent(r.httpClient, entry)
}
