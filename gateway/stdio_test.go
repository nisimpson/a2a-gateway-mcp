package gateway

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestStdioIntegration_ListAgentsEmpty verifies that a freshly started server
// returns an empty array from list_agents when accessed through the MCP protocol.
func TestStdioIntegration_ListAgentsEmpty(t *testing.T) {
	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	ctx := context.Background()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_agents",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool(list_agents) error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	if text != "[]" {
		t.Errorf("expected empty JSON array, got %q", text)
	}
}

// TestStdioIntegration_ConnectListDisconnectFlow tests the full lifecycle of
// connecting an agent, listing it, and disconnecting it through the MCP protocol.
func TestStdioIntegration_ConnectListDisconnectFlow(t *testing.T) {
	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	ctx := context.Background()

	// Step 1: Connect an agent.
	connectResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "connect_agent",
		Arguments: map[string]any{
			"alias":     "test-agent",
			"agent_url": "https://test-agent.example.com",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(connect_agent) error: %v", err)
	}
	if connectResult.IsError {
		t.Fatalf("connect_agent failed: %s", getTextContent(t, connectResult))
	}

	// Step 2: List agents — should contain the connected agent.
	listResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_agents",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool(list_agents) error: %v", err)
	}
	if listResult.IsError {
		t.Fatalf("list_agents failed: %s", getTextContent(t, listResult))
	}

	listText := getTextContent(t, listResult)
	var agents []listAgentEntry
	if err := json.Unmarshal([]byte(listText), &agents); err != nil {
		t.Fatalf("failed to parse list_agents response: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].Alias != "test-agent" {
		t.Errorf("expected alias %q, got %q", "test-agent", agents[0].Alias)
	}
	if agents[0].URL != "https://test-agent.example.com" {
		t.Errorf("expected URL %q, got %q", "https://test-agent.example.com", agents[0].URL)
	}

	// Step 3: Disconnect the agent.
	disconnectResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "disconnect_agent",
		Arguments: map[string]any{
			"alias": "test-agent",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(disconnect_agent) error: %v", err)
	}
	if disconnectResult.IsError {
		t.Fatalf("disconnect_agent failed: %s", getTextContent(t, disconnectResult))
	}

	// Step 4: List agents again — should be empty.
	listResult2, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_agents",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool(list_agents) error: %v", err)
	}
	if listResult2.IsError {
		t.Fatalf("list_agents failed: %s", getTextContent(t, listResult2))
	}

	listText2 := getTextContent(t, listResult2)
	if listText2 != "[]" {
		t.Errorf("expected empty JSON array after disconnect, got %q", listText2)
	}
}

// TestStdioIntegration_MultipleAgentsFlow tests connecting multiple agents,
// verifying sorted listing, and selective disconnection.
func TestStdioIntegration_MultipleAgentsFlow(t *testing.T) {
	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	ctx := context.Background()

	// Connect multiple agents in non-alphabetical order.
	agentsToConnect := []struct {
		alias string
		url   string
	}{
		{"zulu", "https://zulu.example.com"},
		{"alpha", "https://alpha.example.com"},
		{"bravo", "https://bravo.example.com"},
	}

	for _, a := range agentsToConnect {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "connect_agent",
			Arguments: map[string]any{
				"alias":     a.alias,
				"agent_url": a.url,
			},
		})
		if err != nil {
			t.Fatalf("CallTool(connect_agent) for %q error: %v", a.alias, err)
		}
		if result.IsError {
			t.Fatalf("connect_agent for %q failed: %s", a.alias, getTextContent(t, result))
		}
	}

	// List agents — should be sorted alphabetically.
	listResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_agents",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool(list_agents) error: %v", err)
	}

	listText := getTextContent(t, listResult)
	var agents []listAgentEntry
	if err := json.Unmarshal([]byte(listText), &agents); err != nil {
		t.Fatalf("failed to parse list_agents response: %v", err)
	}
	if len(agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(agents))
	}
	if agents[0].Alias != "alpha" || agents[1].Alias != "bravo" || agents[2].Alias != "zulu" {
		t.Errorf("expected sorted order [alpha, bravo, zulu], got [%s, %s, %s]",
			agents[0].Alias, agents[1].Alias, agents[2].Alias)
	}

	// Disconnect one agent.
	disconnectResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "disconnect_agent",
		Arguments: map[string]any{
			"alias": "bravo",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(disconnect_agent) error: %v", err)
	}
	if disconnectResult.IsError {
		t.Fatalf("disconnect_agent failed: %s", getTextContent(t, disconnectResult))
	}

	// List again — should have 2 agents.
	listResult2, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_agents",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool(list_agents) error: %v", err)
	}

	listText2 := getTextContent(t, listResult2)
	var agents2 []listAgentEntry
	if err := json.Unmarshal([]byte(listText2), &agents2); err != nil {
		t.Fatalf("failed to parse list_agents response: %v", err)
	}
	if len(agents2) != 2 {
		t.Fatalf("expected 2 agents after disconnect, got %d", len(agents2))
	}
	if agents2[0].Alias != "alpha" || agents2[1].Alias != "zulu" {
		t.Errorf("expected [alpha, zulu], got [%s, %s]", agents2[0].Alias, agents2[1].Alias)
	}
}

// TestStdioIntegration_DisconnectNonExistent verifies that disconnecting a
// non-existent agent returns an error through the MCP protocol.
func TestStdioIntegration_DisconnectNonExistent(t *testing.T) {
	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	ctx := context.Background()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "disconnect_agent",
		Arguments: map[string]any{
			"alias": "nonexistent",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(disconnect_agent) error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for disconnecting non-existent agent, got success")
	}
}

// TestStdioIntegration_ConnectWithHeaders verifies that headers are stored
// correctly when connecting an agent through the MCP protocol.
func TestStdioIntegration_ConnectWithHeaders(t *testing.T) {
	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	ctx := context.Background()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "connect_agent",
		Arguments: map[string]any{
			"alias":     "auth-agent",
			"agent_url": "https://auth-agent.example.com",
			"headers": map[string]any{
				"Authorization": "Bearer secret-token",
				"X-Api-Key":     "key-123",
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(connect_agent) error: %v", err)
	}
	if result.IsError {
		t.Fatalf("connect_agent failed: %s", getTextContent(t, result))
	}

	// Verify headers are stored in registry.
	entry := srv.registry.Lookup("auth-agent")
	if entry == nil {
		t.Fatal("expected registry entry for 'auth-agent', got nil")
	}
	if entry.Headers["Authorization"] != "Bearer secret-token" {
		t.Errorf("expected Authorization header, got %q", entry.Headers["Authorization"])
	}
	if entry.Headers["X-Api-Key"] != "key-123" {
		t.Errorf("expected X-Api-Key header, got %q", entry.Headers["X-Api-Key"])
	}
}
