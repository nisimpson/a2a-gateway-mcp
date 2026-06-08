package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestBroadcast_MixedRateLimits verifies that broadcasting to a mix of
// rate-limited and non-limited agents produces partial success: successful
// responses for non-limited agents and error results for rate-limited agents.
// Requirements: RLIM-2.3, RLIM-2.4
func TestBroadcast_MixedRateLimits(t *testing.T) {
	// Mock A2A agent that returns a completed task.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:     "task-broadcast-rl",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("broadcast ok")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := NewServer()
	// Register three agents pointing to the same mock server.
	for _, alias := range []string{"agent-ok1", "agent-ok2", "agent-limited"} {
		srv.registry.Connect(alias, agent.URL, nil, "")
		srv.registry.SetCard(alias, &a2a.AgentCard{
			Name: alias,
			SupportedInterfaces: []*a2a.AgentInterface{
				a2a.NewAgentInterface(agent.URL, a2a.TransportProtocolJSONRPC),
			},
		})
	}

	// Set rate limiter only on agent-limited with burst=1 and very low RPS.
	srv.rateLimiters.Set("agent-limited", 0.001, 1)
	// Exhaust the token so the next request will be rate limited.
	srv.rateLimiters.Allow("agent-limited")

	input := BroadcastMessageInput{
		Aliases: []string{"agent-ok1", "agent-ok2", "agent-limited"},
		Message: "hello broadcast",
	}

	result, _, err := srv.handleBroadcastMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected non-error result at top level")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}

	var results map[string]*broadcastResult
	if err := json.Unmarshal([]byte(textContent.Text), &results); err != nil {
		t.Fatalf("failed to parse results: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Non-limited agents should succeed.
	for _, alias := range []string{"agent-ok1", "agent-ok2"} {
		r, exists := results[alias]
		if !exists {
			t.Errorf("missing result for %s", alias)
			continue
		}
		if r.Status != "success" {
			t.Errorf("expected success for %s, got %s (error: %s)", alias, r.Status, r.Error)
		}
		if r.Response != "broadcast ok" {
			t.Errorf("expected response %q for %s, got %q", "broadcast ok", alias, r.Response)
		}
	}

	// Rate-limited agent should have error.
	r, exists := results["agent-limited"]
	if !exists {
		t.Fatal("missing result for agent-limited")
	}
	if r.Status != "error" {
		t.Errorf("expected error for agent-limited, got %s", r.Status)
	}
	if !strings.Contains(r.Error, "rate limited") {
		t.Errorf("expected 'rate limited' in error, got: %s", r.Error)
	}
	if !strings.Contains(r.Error, "agent-limited") {
		t.Errorf("expected alias in error, got: %s", r.Error)
	}
}

// TestBroadcast_AllWithinRateLimit verifies that broadcasting when all agents
// are within their rate limit results in all agents succeeding.
// Requirements: RLIM-2.3, RLIM-2.4
func TestBroadcast_AllWithinRateLimit(t *testing.T) {
	// Mock A2A agent that returns a completed task.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:     "task-broadcast-all-ok",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("all ok response")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := NewServer()
	aliases := []string{"rl-a", "rl-b", "rl-c"}
	for _, alias := range aliases {
		srv.registry.Connect(alias, agent.URL, nil, "")
		srv.registry.SetCard(alias, &a2a.AgentCard{
			Name: alias,
			SupportedInterfaces: []*a2a.AgentInterface{
				a2a.NewAgentInterface(agent.URL, a2a.TransportProtocolJSONRPC),
			},
		})
		// Set rate limiter with generous burst — all requests should pass.
		srv.rateLimiters.Set(alias, 100.0, 10)
	}

	input := BroadcastMessageInput{
		Aliases: aliases,
		Message: "broadcast within limit",
	}

	result, _, err := srv.handleBroadcastMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected non-error result at top level")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}

	var results map[string]*broadcastResult
	if err := json.Unmarshal([]byte(textContent.Text), &results); err != nil {
		t.Fatalf("failed to parse results: %v", err)
	}

	if len(results) != len(aliases) {
		t.Fatalf("expected %d results, got %d", len(aliases), len(results))
	}

	// All agents should succeed.
	for _, alias := range aliases {
		r, exists := results[alias]
		if !exists {
			t.Errorf("missing result for %s", alias)
			continue
		}
		if r.Status != "success" {
			t.Errorf("expected success for %s, got %s (error: %s)", alias, r.Status, r.Error)
		}
		if r.Response != "all ok response" {
			t.Errorf("expected response %q for %s, got %q", "all ok response", alias, r.Response)
		}
	}
}

// TestBroadcast_AllRateLimited verifies that broadcasting when all agents
// are rate limited results in all agents reporting errors.
// Requirements: RLIM-2.3, RLIM-2.4
func TestBroadcast_AllRateLimited(t *testing.T) {
	// Mock A2A agent — should NOT be called when rate limited.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("agent should not be called when all are rate limited")
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:     "task-should-not-see",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("should not see")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := NewServer()
	aliases := []string{"limited-a", "limited-b", "limited-c"}
	for _, alias := range aliases {
		srv.registry.Connect(alias, agent.URL, nil, "")
		srv.registry.SetCard(alias, &a2a.AgentCard{
			Name: alias,
			SupportedInterfaces: []*a2a.AgentInterface{
				a2a.NewAgentInterface(agent.URL, a2a.TransportProtocolJSONRPC),
			},
		})
		// Set rate limiter with burst=1 and very low RPS, then exhaust the token.
		srv.rateLimiters.Set(alias, 0.001, 1)
		srv.rateLimiters.Allow(alias) // exhaust the single token
	}

	input := BroadcastMessageInput{
		Aliases: aliases,
		Message: "broadcast all limited",
	}

	result, _, err := srv.handleBroadcastMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected non-error result at top level")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}

	var results map[string]*broadcastResult
	if err := json.Unmarshal([]byte(textContent.Text), &results); err != nil {
		t.Fatalf("failed to parse results: %v", err)
	}

	if len(results) != len(aliases) {
		t.Fatalf("expected %d results, got %d", len(aliases), len(results))
	}

	// All agents should report rate limit errors.
	for _, alias := range aliases {
		r, exists := results[alias]
		if !exists {
			t.Errorf("missing result for %s", alias)
			continue
		}
		if r.Status != "error" {
			t.Errorf("expected error for %s, got %s", alias, r.Status)
		}
		if !strings.Contains(r.Error, "rate limited") {
			t.Errorf("expected 'rate limited' in error for %s, got: %s", alias, r.Error)
		}
		if !strings.Contains(r.Error, alias) {
			t.Errorf("expected alias %q in error message, got: %s", alias, r.Error)
		}
	}
}
