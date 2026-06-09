package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetAgentCard_AliasResolution(t *testing.T) {
	// Set up a mock agent card endpoint.
	agentCard := map[string]any{
		"name":        "test-agent",
		"description": "A test agent",
		"url":         "http://example.com",
	}
	cardJSON, _ := json.Marshal(agentCard)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/agent-card.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cardJSON)
	}))
	defer ts.Close()

	srv := NewServer()
	srv.registry.Connect("my-agent", ts.URL, map[string]string{"X-Api-Key": "secret"}, "")

	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	ctx := context.Background()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_agent_card",
		Arguments: map[string]any{"agent": "my-agent"},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Verify the response contains the agent card JSON.
	if len(result.Content) == 0 {
		t.Fatal("expected at least one content item")
	}
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(textContent.Text), &parsed); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if parsed["name"] != "test-agent" {
		t.Errorf("expected name %q, got %q", "test-agent", parsed["name"])
	}
}

func TestGetAgentCard_AliasResolution_HeadersApplied(t *testing.T) {
	// Verify that stored headers are sent when using alias-based resolution.
	var receivedHeaders http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"test"}`))
	}))
	defer ts.Close()

	srv := NewServer()
	srv.registry.Connect("my-agent", ts.URL, map[string]string{"X-Api-Key": "my-secret-key"}, "")

	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	ctx := context.Background()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_agent_card",
		Arguments: map[string]any{"agent": "my-agent"},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	if receivedHeaders.Get("X-Api-Key") != "my-secret-key" {
		t.Errorf("expected X-Api-Key header to be %q, got %q", "my-secret-key", receivedHeaders.Get("X-Api-Key"))
	}
}

func TestGetAgentCard_URLBasedAccess(t *testing.T) {
	// Set up a mock agent card endpoint.
	agentCard := map[string]any{
		"name":        "url-agent",
		"description": "An agent accessed by URL",
	}
	cardJSON, _ := json.Marshal(agentCard)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/agent-card.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cardJSON)
	}))
	defer ts.Close()

	srv := NewServer()

	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	ctx := context.Background()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_agent_card",
		Arguments: map[string]any{"agent": ts.URL},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	if len(result.Content) == 0 {
		t.Fatal("expected at least one content item")
	}
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(textContent.Text), &parsed); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if parsed["name"] != "url-agent" {
		t.Errorf("expected name %q, got %q", "url-agent", parsed["name"])
	}
}

func TestGetAgentCard_UnreachableAgent(t *testing.T) {
	// Use a server that immediately closes to simulate unreachable.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	unreachableURL := ts.URL
	ts.Close() // Close immediately so it's unreachable.

	srv := NewServer(WithHTTPClient(&http.Client{Timeout: 1 * time.Second}))

	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	ctx := context.Background()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_agent_card",
		Arguments: map[string]any{"agent": unreachableURL},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for unreachable agent, got success")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if textContent.Text == "" {
		t.Error("expected non-empty error message")
	}
}

func TestGetAgentCard_MalformedJSON(t *testing.T) {
	// Return invalid JSON from the agent card endpoint.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{not valid json`))
	}))
	defer ts.Close()

	srv := NewServer()

	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	ctx := context.Background()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_agent_card",
		Arguments: map[string]any{"agent": ts.URL},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for malformed JSON, got success")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if textContent.Text == "" {
		t.Error("expected non-empty error message for parse failure")
	}
}

func TestGetAgentCard_EmptyAgentIdentifier(t *testing.T) {
	srv := NewServer()

	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	ctx := context.Background()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_agent_card",
		Arguments: map[string]any{"agent": ""},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for empty agent identifier, got success")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if textContent.Text == "" {
		t.Error("expected non-empty error message")
	}
}

func TestGetAgentCard_Non200Status(t *testing.T) {
	// Return a 404 from the agent card endpoint.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer ts.Close()

	srv := NewServer()

	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	ctx := context.Background()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_agent_card",
		Arguments: map[string]any{"agent": ts.URL},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for non-200 status, got success")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if textContent.Text == "" {
		t.Error("expected non-empty error message for non-200 status")
	}
}

func TestGetAgentCard_URLBasedNoHeaders(t *testing.T) {
	// Verify that URL-based access does NOT apply headers from a registry entry
	// with the same URL.
	var receivedHeaders http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"test"}`))
	}))
	defer ts.Close()

	srv := NewServer()
	// Register an agent with the same URL and custom headers.
	srv.registry.Connect("same-url-agent", ts.URL, map[string]string{"X-Secret": "should-not-appear"}, "")

	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	ctx := context.Background()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_agent_card",
		Arguments: map[string]any{"agent": ts.URL},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// The X-Secret header should NOT be present since we used a URL, not an alias.
	if receivedHeaders.Get("X-Secret") != "" {
		t.Errorf("expected no X-Secret header for URL-based access, got %q", receivedHeaders.Get("X-Secret"))
	}
}
