package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
)

func TestClientResolver_DynamicHeadersFromRegistry(t *testing.T) {
	// Set up a test agent server that captures the Authorization header.
	var receivedAuth string
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		// Return a valid JSON-RPC response.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"1","result":{"id":"t1","status":{"state":"completed"},"contextId":"","artifacts":null}}`))
	}))
	defer agent.Close()

	// Create registry and register agent with initial token.
	reg := NewAgentRegistry()
	reg.Connect("my-agent", agent.URL, map[string]string{"Authorization": "Bearer token-v1"}, "")

	// Create resolver backed by the registry.
	resolver := NewClientResolver(reg, &http.Client{})

	// Resolve the client — this creates and caches the client.
	resolved := &ResolveResult{
		URL:     agent.URL,
		Headers: map[string]string{"Authorization": "Bearer token-v1"},
		IsAlias: true,
		Alias:   "my-agent",
	}
	client, err := resolver.Resolve(context.Background(), resolved)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Make a request — should use initial token.
	_, _ = client.GetTask(context.Background(), &a2a.GetTaskRequest{ID: "task-1"})
	if receivedAuth != "Bearer token-v1" {
		t.Errorf("first request: expected 'Bearer token-v1', got %q", receivedAuth)
	}

	// Simulate token rotation: update registry headers (as connect_agent would).
	reg.Connect("my-agent", agent.URL, map[string]string{"Authorization": "Bearer token-v2"}, "")

	// Make another request using the SAME cached client — should pick up new token.
	_, _ = client.GetTask(context.Background(), &a2a.GetTaskRequest{ID: "task-1"})
	if receivedAuth != "Bearer token-v2" {
		t.Errorf("second request: expected 'Bearer token-v2', got %q", receivedAuth)
	}
}

func TestClientResolver_EvictForcesFreshClient(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"1","result":{"id":"t1","status":{"state":"completed"},"contextId":"","artifacts":null}}`))
	}))
	defer agent.Close()

	reg := NewAgentRegistry()
	reg.Connect("agent-a", agent.URL, nil, "")

	resolver := NewClientResolver(reg, &http.Client{})
	resolved := &ResolveResult{URL: agent.URL, IsAlias: true, Alias: "agent-a"}

	// Resolve twice — should get same cached client.
	client1, _ := resolver.Resolve(context.Background(), resolved)
	client2, _ := resolver.Resolve(context.Background(), resolved)
	if client1 != client2 {
		t.Error("expected same cached client instance")
	}

	// Evict and resolve again — should get a new client.
	resolver.Evict(agent.URL)
	client3, _ := resolver.Resolve(context.Background(), resolved)
	if client3 == client1 {
		t.Error("expected new client after eviction")
	}
}

func newResolverTestClient(t *testing.T, url string) *a2aclient.Client {
	t.Helper()
	endpoints := []*a2a.AgentInterface{
		a2a.NewAgentInterface(url, a2a.TransportProtocolJSONRPC),
	}
	client, err := a2aclient.NewFromEndpoints(context.Background(), endpoints)
	if err != nil {
		t.Fatalf("failed to create test client: %v", err)
	}
	return client
}
