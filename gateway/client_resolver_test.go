package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
)

// newTestJSONRPCServer creates a test HTTP server that responds to JSON-RPC
// requests with a simple task response.
func newTestJSONRPCServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
			ID      string          `json:"id"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid JSON-RPC", http.StatusBadRequest)
			return
		}

		// Return a simple task response wrapped in JSON-RPC envelope.
		task := a2a.Task{
			ID: "test-task-id",
			Status: a2a.TaskStatus{
				State: a2a.TaskStateCompleted,
			},
		}

		type jsonrpcResponse struct {
			JSONRPC string `json:"jsonrpc"`
			ID      string `json:"id"`
			Result  any    `json:"result"`
		}

		// Wrap task in a StreamResponse-compatible format that the SDK expects.
		// The SDK's jsonrpcTransport.SendMessage unmarshals the result as a StreamResponse.
		streamResp := map[string]any{
			"kind": "task",
			"task": task,
		}

		resp := jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  streamResp,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// newSendMessageRequest creates a minimal SendMessageRequest for testing.
func newSendMessageRequest() *a2a.SendMessageRequest {
	return &a2a.SendMessageRequest{
		Message: &a2a.Message{
			Role:  a2a.MessageRoleUser,
			Parts: a2a.ContentParts{a2a.NewTextPart("hello")},
		},
	}
}

func TestClientResolver_NilCardFallsBackToJSONRPC(t *testing.T) {
	ts := newTestJSONRPCServer(t)
	defer ts.Close()

	registry := NewAgentRegistry()
	registry.Connect("test-agent", ts.URL, nil)
	// Card is nil by default — no SetCard call.

	resolver := newClientResolver(registry, &http.Client{Timeout: 5 * time.Second})

	resolved := &ResolveResult{
		URL:     ts.URL,
		IsAlias: true,
	}

	client, err := resolver.Resolve(context.Background(), resolved)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Verify client works by sending a message.
	result, err := client.SendMessage(context.Background(), newSendMessageRequest())
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestClientResolver_CardWithJSONRPCInterface(t *testing.T) {
	ts := newTestJSONRPCServer(t)
	defer ts.Close()

	registry := NewAgentRegistry()
	registry.Connect("test-agent", ts.URL, nil)

	// Set card with JSONRPC interface.
	card := &a2a.AgentCard{
		Name: "test-agent",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(ts.URL, a2a.TransportProtocolJSONRPC),
		},
	}
	registry.SetCard("test-agent", card)

	resolver := newClientResolver(registry, &http.Client{Timeout: 5 * time.Second})

	resolved := &ResolveResult{
		URL:     ts.URL,
		IsAlias: true,
	}

	client, err := resolver.Resolve(context.Background(), resolved)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Verify client works.
	result, err := client.SendMessage(context.Background(), newSendMessageRequest())
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestClientResolver_CacheReturnsSameInstance(t *testing.T) {
	ts := newTestJSONRPCServer(t)
	defer ts.Close()

	registry := NewAgentRegistry()
	registry.Connect("test-agent", ts.URL, nil)

	resolver := newClientResolver(registry, &http.Client{Timeout: 5 * time.Second})

	resolved := &ResolveResult{
		URL:     ts.URL,
		IsAlias: true,
	}

	client1, err := resolver.Resolve(context.Background(), resolved)
	if err != nil {
		t.Fatalf("first Resolve failed: %v", err)
	}

	client2, err := resolver.Resolve(context.Background(), resolved)
	if err != nil {
		t.Fatalf("second Resolve failed: %v", err)
	}

	// Pointer equality — same instance returned from cache.
	if client1 != client2 {
		t.Error("expected cached client to be the same instance on second call")
	}
}

func TestClientResolver_URLBasedResolutionDefaultsToJSONRPC(t *testing.T) {
	ts := newTestJSONRPCServer(t)
	defer ts.Close()

	registry := NewAgentRegistry()
	// No agent registered — this is URL-based resolution.

	resolver := newClientResolver(registry, &http.Client{Timeout: 5 * time.Second})

	resolved := &ResolveResult{
		URL:     ts.URL,
		IsAlias: false, // URL-based, not alias-based
	}

	client, err := resolver.Resolve(context.Background(), resolved)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Verify client works with JSON-RPC.
	result, err := client.SendMessage(context.Background(), newSendMessageRequest())
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestClientResolver_CustomHeadersPropagated(t *testing.T) {
	var receivedHeaders http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()

		body, _ := io.ReadAll(r.Body)
		r.Body.Close()

		var req struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(body, &req)

		task := a2a.Task{
			ID:     "test-task",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
		}
		streamResp := map[string]any{
			"kind": "task",
			"task": task,
		}
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": streamResp}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	headers := map[string]string{
		"X-Api-Key":    "secret-key-123",
		"X-Custom-Hdr": "custom-value",
	}

	registry := NewAgentRegistry()
	registry.Connect("test-agent", ts.URL, headers)

	resolver := newClientResolver(registry, &http.Client{Timeout: 5 * time.Second})

	resolved := &ResolveResult{
		URL:     ts.URL,
		Headers: headers,
		IsAlias: true,
	}

	client, err := resolver.Resolve(context.Background(), resolved)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Send a message to trigger the HTTP request.
	_, err = client.SendMessage(context.Background(), newSendMessageRequest())
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	// Verify custom headers were received.
	if receivedHeaders.Get("X-Api-Key") != "secret-key-123" {
		t.Errorf("expected X-Api-Key header, got %q", receivedHeaders.Get("X-Api-Key"))
	}
	if receivedHeaders.Get("X-Custom-Hdr") != "custom-value" {
		t.Errorf("expected X-Custom-Hdr header, got %q", receivedHeaders.Get("X-Custom-Hdr"))
	}
}

func TestClientResolver_ConcurrentResolve(t *testing.T) {
	ts := newTestJSONRPCServer(t)
	defer ts.Close()

	registry := NewAgentRegistry()
	registry.Connect("test-agent", ts.URL, nil)

	resolver := newClientResolver(registry, &http.Client{Timeout: 5 * time.Second})

	resolved := &ResolveResult{
		URL:     ts.URL,
		IsAlias: true,
	}

	// Resolve concurrently to test thread safety.
	var wg sync.WaitGroup
	results := make([]*a2aclient.Client, 10)
	errs := make([]error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c, err := resolver.Resolve(context.Background(), resolved)
			results[idx] = c
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	// All should succeed and return the same instance.
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent Resolve[%d] failed: %v", i, err)
		}
		if results[i] == nil {
			t.Fatalf("concurrent Resolve[%d] returned nil client", i)
		}
	}

	// All should be the same cached instance.
	first := results[0]
	for i := 1; i < len(results); i++ {
		if results[i] != first {
			t.Errorf("concurrent Resolve[%d] returned different instance", i)
		}
	}
}
