package gateway

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// callTool is a test helper that calls a tool by name with the given arguments
// via an MCP client session.
func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx := context.Background()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool(%q) returned error: %v", name, err)
	}
	return result
}

// getTextContent extracts the text from the first TextContent item in a result.
func getTextContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("expected at least one content item, got none")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	return tc.Text
}

// --- connect_agent tests ---

func TestConnectAgent_ValidInputs(t *testing.T) {
	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	result := callTool(t, session, "connect_agent", map[string]any{
		"alias":     "my-agent",
		"agent_url": "https://agent.example.com",
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	if text == "" {
		t.Fatal("expected non-empty confirmation text")
	}

	// Verify registry entry was created.
	entry := srv.registry.Lookup("my-agent")
	if entry == nil {
		t.Fatal("expected registry entry for 'my-agent', got nil")
	}
	if entry.URL != "https://agent.example.com" {
		t.Errorf("expected URL %q, got %q", "https://agent.example.com", entry.URL)
	}
}

func TestConnectAgent_OverwriteSameURL(t *testing.T) {
	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	// Connect first time.
	callTool(t, session, "connect_agent", map[string]any{
		"alias":     "my-agent",
		"agent_url": "https://agent.example.com",
	})

	// Set a context store entry.
	srv.contextStore.Set("my-agent", "ctx-123")

	// Connect again with same URL — context should NOT be cleared.
	result := callTool(t, session, "connect_agent", map[string]any{
		"alias":     "my-agent",
		"agent_url": "https://agent.example.com",
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	// Context should still be present since URL didn't change.
	if got := srv.contextStore.Get("my-agent"); got != "ctx-123" {
		t.Errorf("expected context to remain %q, got %q", "ctx-123", got)
	}
}

func TestConnectAgent_OverwriteDifferentURL_ClearsContext(t *testing.T) {
	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	// Connect first time.
	callTool(t, session, "connect_agent", map[string]any{
		"alias":     "my-agent",
		"agent_url": "https://agent1.example.com",
	})

	// Set a context store entry.
	srv.contextStore.Set("my-agent", "ctx-123")

	// Connect again with DIFFERENT URL — context should be cleared.
	result := callTool(t, session, "connect_agent", map[string]any{
		"alias":     "my-agent",
		"agent_url": "https://agent2.example.com",
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	// Context should be cleared since URL changed.
	if got := srv.contextStore.Get("my-agent"); got != "" {
		t.Errorf("expected context to be cleared, got %q", got)
	}

	// Verify new URL is stored.
	entry := srv.registry.Lookup("my-agent")
	if entry == nil {
		t.Fatal("expected registry entry for 'my-agent', got nil")
	}
	if entry.URL != "https://agent2.example.com" {
		t.Errorf("expected URL %q, got %q", "https://agent2.example.com", entry.URL)
	}
}

func TestConnectAgent_WithHeaders(t *testing.T) {
	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	headers := map[string]any{
		"Authorization": "Bearer token123",
		"X-Custom":      "value",
	}

	result := callTool(t, session, "connect_agent", map[string]any{
		"alias":     "my-agent",
		"agent_url": "https://agent.example.com",
		"headers":   headers,
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	entry := srv.registry.Lookup("my-agent")
	if entry == nil {
		t.Fatal("expected registry entry for 'my-agent', got nil")
	}
	if entry.Headers["Authorization"] != "Bearer token123" {
		t.Errorf("expected Authorization header %q, got %q", "Bearer token123", entry.Headers["Authorization"])
	}
	if entry.Headers["X-Custom"] != "value" {
		t.Errorf("expected X-Custom header %q, got %q", "value", entry.Headers["X-Custom"])
	}
}

func TestConnectAgent_InvalidAlias(t *testing.T) {
	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	tests := []struct {
		name  string
		alias string
	}{
		{"empty alias", ""},
		{"uppercase", "MyAgent"},
		{"spaces", "my agent"},
		{"special chars", "my_agent!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := callTool(t, session, "connect_agent", map[string]any{
				"alias":     tt.alias,
				"agent_url": "https://agent.example.com",
			})
			if !result.IsError {
				t.Errorf("expected error for alias %q, got success", tt.alias)
			}
		})
	}
}

func TestConnectAgent_InvalidURL(t *testing.T) {
	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	tests := []struct {
		name string
		url  string
	}{
		{"empty URL", ""},
		{"ftp scheme", "ftp://agent.example.com"},
		{"no scheme", "agent.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := callTool(t, session, "connect_agent", map[string]any{
				"alias":     "my-agent",
				"agent_url": tt.url,
			})
			if !result.IsError {
				t.Errorf("expected error for URL %q, got success", tt.url)
			}
		})
	}
}

func TestConnectAgent_TooManyHeaders(t *testing.T) {
	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	// Create 21 headers (exceeds max of 20).
	headers := make(map[string]any)
	for i := 0; i < 21; i++ {
		headers["Header-"+string(rune('A'+i))] = "value"
	}

	result := callTool(t, session, "connect_agent", map[string]any{
		"alias":     "my-agent",
		"agent_url": "https://agent.example.com",
		"headers":   headers,
	})

	if !result.IsError {
		t.Error("expected error for too many headers, got success")
	}
}

// --- disconnect_agent tests ---

func TestDisconnectAgent_Existing(t *testing.T) {
	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	// Connect an agent first.
	callTool(t, session, "connect_agent", map[string]any{
		"alias":     "my-agent",
		"agent_url": "https://agent.example.com",
	})

	// Set context.
	srv.contextStore.Set("my-agent", "ctx-456")

	// Disconnect.
	result := callTool(t, session, "disconnect_agent", map[string]any{
		"alias": "my-agent",
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	// Verify registry entry removed.
	if entry := srv.registry.Lookup("my-agent"); entry != nil {
		t.Error("expected registry entry to be removed")
	}

	// Verify context store entry removed.
	if got := srv.contextStore.Get("my-agent"); got != "" {
		t.Errorf("expected context to be cleared, got %q", got)
	}
}

func TestDisconnectAgent_NotFound(t *testing.T) {
	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	result := callTool(t, session, "disconnect_agent", map[string]any{
		"alias": "nonexistent",
	})

	if !result.IsError {
		t.Error("expected error for non-existing alias, got success")
	}
}

func TestDisconnectAgent_EmptyAlias(t *testing.T) {
	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	result := callTool(t, session, "disconnect_agent", map[string]any{
		"alias": "",
	})

	if !result.IsError {
		t.Error("expected error for empty alias, got success")
	}
}

// --- list_agents tests ---

func TestListAgents_EmptyRegistry(t *testing.T) {
	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	result := callTool(t, session, "list_agents", map[string]any{})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	if text != "[]" {
		t.Errorf("expected empty JSON array %q, got %q", "[]", text)
	}
}

func TestListAgents_MultipleAgents_Sorted(t *testing.T) {
	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	// Connect agents in non-alphabetical order.
	callTool(t, session, "connect_agent", map[string]any{
		"alias":     "zebra",
		"agent_url": "https://zebra.example.com",
	})
	callTool(t, session, "connect_agent", map[string]any{
		"alias":     "alpha",
		"agent_url": "https://alpha.example.com",
	})
	callTool(t, session, "connect_agent", map[string]any{
		"alias":     "middle",
		"agent_url": "https://middle.example.com",
	})

	result := callTool(t, session, "list_agents", map[string]any{})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)

	var agents []listAgentEntry
	if err := json.Unmarshal([]byte(text), &agents); err != nil {
		t.Fatalf("failed to parse list response: %v", err)
	}

	if len(agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(agents))
	}

	// Verify sorted order.
	expected := []struct {
		alias string
		url   string
	}{
		{"alpha", "https://alpha.example.com"},
		{"middle", "https://middle.example.com"},
		{"zebra", "https://zebra.example.com"},
	}

	for i, exp := range expected {
		if agents[i].Alias != exp.alias {
			t.Errorf("agent[%d]: expected alias %q, got %q", i, exp.alias, agents[i].Alias)
		}
		if agents[i].URL != exp.url {
			t.Errorf("agent[%d]: expected URL %q, got %q", i, exp.url, agents[i].URL)
		}
	}
}
