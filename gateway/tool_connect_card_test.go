package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestConnectAgent_FetchesCardOnSuccess verifies that a successful card fetch
// stores the AgentCard in the registry entry.
func TestConnectAgent_FetchesCardOnSuccess(t *testing.T) {
	agentCard := map[string]any{
		"name":        "test-agent",
		"description": "A test agent",
		"url":         "http://example.com",
	}
	cardJSON, _ := json.Marshal(agentCard)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent.json" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(cardJSON)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	result := callTool(t, session, "connect_agent", map[string]any{
		"alias":     "card-agent",
		"agent_url": ts.URL,
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	// Verify the card was stored.
	entry := srv.registry.Lookup("card-agent")
	if entry == nil {
		t.Fatal("expected registry entry, got nil")
	}
	if entry.Card == nil {
		t.Fatal("expected Card to be stored, got nil")
	}
	if entry.Card.Name != "test-agent" {
		t.Errorf("expected card name %q, got %q", "test-agent", entry.Card.Name)
	}
}

// TestConnectAgent_CardFetch404_StillRegisters verifies that a 404 from the
// agent card endpoint does not prevent agent registration.
func TestConnectAgent_CardFetch404_StillRegisters(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer ts.Close()

	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	result := callTool(t, session, "connect_agent", map[string]any{
		"alias":     "no-card-agent",
		"agent_url": ts.URL,
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	entry := srv.registry.Lookup("no-card-agent")
	if entry == nil {
		t.Fatal("expected registry entry, got nil")
	}
	if entry.Card != nil {
		t.Errorf("expected Card to be nil on 404, got %+v", entry.Card)
	}
}

// TestConnectAgent_CardFetchTimeout_StillRegisters verifies that a timeout
// during card fetch does not prevent agent registration.
func TestConnectAgent_CardFetchTimeout_StillRegisters(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a slow server that exceeds the client timeout.
		time.Sleep(2 * time.Second)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"slow-agent"}`))
	}))
	defer ts.Close()

	// Use a very short timeout to trigger timeout quickly.
	srv := NewServer(WithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond}))
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	result := callTool(t, session, "connect_agent", map[string]any{
		"alias":     "timeout-agent",
		"agent_url": ts.URL,
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	entry := srv.registry.Lookup("timeout-agent")
	if entry == nil {
		t.Fatal("expected registry entry, got nil")
	}
	if entry.Card != nil {
		t.Errorf("expected Card to be nil on timeout, got %+v", entry.Card)
	}
}

// TestConnectAgent_CardFetchInvalidJSON_StillRegisters verifies that invalid
// JSON from the card endpoint does not prevent agent registration.
func TestConnectAgent_CardFetchInvalidJSON_StillRegisters(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{not valid json`))
	}))
	defer ts.Close()

	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	result := callTool(t, session, "connect_agent", map[string]any{
		"alias":     "bad-json-agent",
		"agent_url": ts.URL,
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	entry := srv.registry.Lookup("bad-json-agent")
	if entry == nil {
		t.Fatal("expected registry entry, got nil")
	}
	if entry.Card != nil {
		t.Errorf("expected Card to be nil on invalid JSON, got %+v", entry.Card)
	}
}

// TestConnectAgent_CardFetchUsesAgentHeaders verifies that agent-specific
// headers are sent when fetching the agent card.
func TestConnectAgent_CardFetchUsesAgentHeaders(t *testing.T) {
	var receivedHeaders http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"header-agent"}`))
	}))
	defer ts.Close()

	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	result := callTool(t, session, "connect_agent", map[string]any{
		"alias":     "header-agent",
		"agent_url": ts.URL,
		"headers": map[string]any{
			"Authorization": "Bearer secret-token",
			"X-Custom":      "custom-value",
		},
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	// Verify the headers were sent.
	if receivedHeaders.Get("Authorization") != "Bearer secret-token" {
		t.Errorf("expected Authorization header %q, got %q", "Bearer secret-token", receivedHeaders.Get("Authorization"))
	}
	if receivedHeaders.Get("X-Custom") != "custom-value" {
		t.Errorf("expected X-Custom header %q, got %q", "custom-value", receivedHeaders.Get("X-Custom"))
	}
}

// TestConnectAgent_CardFetchUnreachable_StillRegisters verifies that an
// unreachable server does not prevent agent registration.
func TestConnectAgent_CardFetchUnreachable_StillRegisters(t *testing.T) {
	// Create and immediately close a server to get an unreachable URL.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	unreachableURL := ts.URL
	ts.Close()

	srv := NewServer(WithHTTPClient(&http.Client{Timeout: 1 * time.Second}))
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	result := callTool(t, session, "connect_agent", map[string]any{
		"alias":     "unreachable-agent",
		"agent_url": unreachableURL,
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	entry := srv.registry.Lookup("unreachable-agent")
	if entry == nil {
		t.Fatal("expected registry entry, got nil")
	}
	if entry.Card != nil {
		t.Errorf("expected Card to be nil for unreachable agent, got %+v", entry.Card)
	}
}
