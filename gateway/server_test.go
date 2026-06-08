package gateway

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNewServer_EmptyRegistry(t *testing.T) {
	srv := NewServer()
	if srv.registry.Len() != 0 {
		t.Errorf("expected empty registry, got %d entries", srv.registry.Len())
	}
}

func TestNewServer_EmptyContextStore(t *testing.T) {
	srv := NewServer()
	// Verify context store is initialized and empty by checking a non-existent key.
	if got := srv.contextStore.Get("nonexistent"); got != "" {
		t.Errorf("expected empty context store, got %q for nonexistent key", got)
	}
}

func TestNewServer_DefaultHTTPClientTimeout(t *testing.T) {
	srv := NewServer()
	if srv.httpClient.Timeout != 30*time.Second {
		t.Errorf("expected default HTTP client timeout of 30s, got %v", srv.httpClient.Timeout)
	}
}

func TestNewServer_DefaultNameAndVersion(t *testing.T) {
	srv := NewServer()
	// Verify by connecting a client and checking server info.
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	info := session.InitializeResult().ServerInfo
	if info.Name != "a2a-gateway-mcp" {
		t.Errorf("expected server name %q, got %q", "a2a-gateway-mcp", info.Name)
	}
	if info.Version != "0.1.0" {
		t.Errorf("expected server version %q, got %q", "0.1.0", info.Version)
	}
}

func TestWithHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 60 * time.Second}
	srv := NewServer(WithHTTPClient(custom))
	if srv.httpClient != custom {
		t.Error("expected custom HTTP client to be set")
	}
	if srv.httpClient.Timeout != 60*time.Second {
		t.Errorf("expected custom timeout of 60s, got %v", srv.httpClient.Timeout)
	}
}

func TestWithName(t *testing.T) {
	srv := NewServer(WithName("custom-name"))
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	info := session.InitializeResult().ServerInfo
	if info.Name != "custom-name" {
		t.Errorf("expected server name %q, got %q", "custom-name", info.Name)
	}
}

func TestWithVersion(t *testing.T) {
	srv := NewServer(WithVersion("2.0.0"))
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	info := session.InitializeResult().ServerInfo
	if info.Version != "2.0.0" {
		t.Errorf("expected server version %q, got %q", "2.0.0", info.Version)
	}
}

func TestMCPServer_Accessor(t *testing.T) {
	srv := NewServer()
	if srv.MCPServer() == nil {
		t.Error("expected MCPServer() to return non-nil")
	}
	if srv.MCPServer() != srv.mcpServer {
		t.Error("expected MCPServer() to return the same mcp.Server instance")
	}
}

func TestNewServer_AllToolsRegistered(t *testing.T) {
	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	ctx := context.Background()
	expectedTools := map[string]bool{
		"connect_agent":     false,
		"disconnect_agent":  false,
		"list_agents":       false,
		"get_agent_card":    false,
		"send_message":      false,
		"broadcast_message": false,
		"discover_agents":   false,
	}

	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("error listing tools: %v", err)
		}
		if _, ok := expectedTools[tool.Name]; ok {
			expectedTools[tool.Name] = true
		}
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("expected tool %q to be registered, but it was not found", name)
		}
	}
}

func TestNewServer_ToolDescriptions(t *testing.T) {
	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	ctx := context.Background()

	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("error listing tools: %v", err)
		}
		if len(tool.Description) < 10 {
			t.Errorf("tool %q has description shorter than 10 chars: %q", tool.Name, tool.Description)
		}
	}
}

// connectTestClient creates an in-memory MCP client session connected to the server.
func connectTestClient(t *testing.T, server *mcp.Server) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("failed to connect server: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("failed to connect client: %v", err)
	}
	return session
}

func TestNewServer_DefaultHealthTracker(t *testing.T) {
	srv := NewServer()
	if srv.healthTracker == nil {
		t.Fatal("expected healthTracker to be initialized")
	}
	if !srv.healthTracker.IsEnabled() {
		t.Error("expected health tracking to be enabled by default")
	}
	// Default threshold is 3: need 3 failures to become unhealthy.
	srv.healthTracker.Reset("test-agent")
	srv.healthTracker.RecordFailure("test-agent")
	srv.healthTracker.RecordFailure("test-agent")
	state := srv.healthTracker.Get("test-agent")
	if state.Status != HealthStatusUnknown {
		t.Errorf("expected unknown after 2 failures (threshold=3), got %s", state.Status)
	}
	srv.healthTracker.RecordFailure("test-agent")
	state = srv.healthTracker.Get("test-agent")
	if state.Status != HealthStatusUnhealthy {
		t.Errorf("expected unhealthy after 3 failures, got %s", state.Status)
	}
}

func TestWithHealthCheck_CustomThreshold(t *testing.T) {
	srv := NewServer(WithHealthCheck(HealthCheckOptions{FailureThreshold: 5}))
	if srv.healthTracker == nil {
		t.Fatal("expected healthTracker to be initialized")
	}
	if !srv.healthTracker.IsEnabled() {
		t.Error("expected health tracking to be enabled with threshold 5")
	}
	// Verify threshold is 5.
	srv.healthTracker.Reset("agent")
	for i := 0; i < 4; i++ {
		srv.healthTracker.RecordFailure("agent")
	}
	state := srv.healthTracker.Get("agent")
	if state.Status == HealthStatusUnhealthy {
		t.Error("expected agent to still be non-unhealthy after 4 failures with threshold=5")
	}
	srv.healthTracker.RecordFailure("agent")
	state = srv.healthTracker.Get("agent")
	if state.Status != HealthStatusUnhealthy {
		t.Errorf("expected unhealthy after 5 failures, got %s", state.Status)
	}
}

func TestWithHealthCheck_ZeroThresholdDisables(t *testing.T) {
	srv := NewServer(WithHealthCheck(HealthCheckOptions{FailureThreshold: 0}))
	if srv.healthTracker == nil {
		t.Fatal("expected healthTracker to be initialized")
	}
	if srv.healthTracker.IsEnabled() {
		t.Error("expected health tracking to be disabled with threshold 0")
	}
}

func TestWithHealthCheck_NegativeThresholdTreatedAsZero(t *testing.T) {
	srv := NewServer(WithHealthCheck(HealthCheckOptions{FailureThreshold: -5}))
	if srv.healthTracker == nil {
		t.Fatal("expected healthTracker to be initialized")
	}
	if srv.healthTracker.IsEnabled() {
		t.Error("expected health tracking to be disabled with negative threshold")
	}
}

func TestNewServer_DefaultPingStrategy(t *testing.T) {
	srv := NewServer()
	if srv.pingStrategy == nil {
		t.Fatal("expected pingStrategy to be initialized")
	}
	// Verify it's a DefaultPingStrategy wrapping the server's HTTP client.
	dps, ok := srv.pingStrategy.(*DefaultPingStrategy)
	if !ok {
		t.Fatalf("expected *DefaultPingStrategy, got %T", srv.pingStrategy)
	}
	if dps.client != srv.httpClient {
		t.Error("expected DefaultPingStrategy to reuse the server's httpClient")
	}
}

func TestWithHealthCheck_CustomPingStrategy(t *testing.T) {
	custom := &mockPingStrategy{}
	srv := NewServer(WithHealthCheck(HealthCheckOptions{
		FailureThreshold: 3,
		PingStrategy:     custom,
	}))
	if srv.pingStrategy != custom {
		t.Error("expected custom ping strategy to be set")
	}
}

// mockPingStrategy is a test double for PingStrategy.
type mockPingStrategy struct{}

func (m *mockPingStrategy) Ping(_ context.Context, _ PingTarget) PingResult {
	return PingResult{Reachable: true}
}
