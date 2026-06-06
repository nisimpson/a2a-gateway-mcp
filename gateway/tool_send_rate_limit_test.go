package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestSendMessage_WithinRateLimit verifies that a send_message request within
// the rate limit proceeds successfully to the target agent.
// Requirements: RLIM-1.4, RLIM-5.1
func TestSendMessage_WithinRateLimit(t *testing.T) {
	// Mock A2A agent that returns a completed task.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:        "task-rl-1",
			ContextID: "ctx-rl-1",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("success response")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("rl-agent", agent.URL)
	// Set a rate limiter with burst=5 — one request should be well within limit.
	srv.rateLimiters.Set("rl-agent", 10.0, 5)

	input := SendMessageInput{
		Agent:   "rl-agent",
		Message: "hello",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", extractText(t, result))
	}

	text := extractText(t, result)
	if text != "success response" {
		t.Errorf("expected %q, got %q", "success response", text)
	}
}

// TestSendMessage_RateLimited verifies that a send_message request that exceeds
// the rate limit returns an error containing the alias and retry time.
// Requirements: RLIM-2.1, RLIM-2.2
func TestSendMessage_RateLimited(t *testing.T) {
	// Mock agent — should NOT be called when rate limited.
	var agentCalls atomic.Int32
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agentCalls.Add(1)
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:        "task-rl-2",
			ContextID: "ctx-rl-2",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("should not see this on second call")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("limited-agent", agent.URL)
	// Set burst=1 with very low RPS so the token doesn't refill between calls.
	srv.rateLimiters.Set("limited-agent", 0.001, 1)

	input := SendMessageInput{
		Agent:   "limited-agent",
		Message: "hello",
	}

	// First send: should succeed (consumes the single token).
	result1, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error on first send: %v", err)
	}
	if result1.IsError {
		t.Fatalf("expected first send to succeed, got error: %s", extractText(t, result1))
	}

	// Second send: should be rate limited (bucket is empty).
	result2, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error on second send: %v", err)
	}
	if !result2.IsError {
		t.Fatal("expected second send to be rate limited, got success")
	}

	text := extractText(t, result2)
	// Verify error message contains required information.
	if !strings.Contains(text, "rate limited") {
		t.Errorf("error message should contain 'rate limited', got: %s", text)
	}
	if !strings.Contains(text, "limited-agent") {
		t.Errorf("error message should contain the alias 'limited-agent', got: %s", text)
	}
	if !strings.Contains(text, "retry after") {
		t.Errorf("error message should contain 'retry after', got: %s", text)
	}
}

// TestSendMessage_DirectURL_NoRateLimit verifies that sending by direct URL
// bypasses rate limiting entirely, even when a global default is configured.
// Requirements: RLIM-5.2
func TestSendMessage_DirectURL_NoRateLimit(t *testing.T) {
	// Mock A2A agent that returns a completed task.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:        "task-url-1",
			ContextID: "ctx-url-1",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("url response")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	// Create server with a global default rate limit.
	srv := NewServer(WithRateLimit(1.0, 1))

	// Send by direct URL — should not be rate limited regardless of global default.
	input := SendMessageInput{
		Agent:   agent.URL,
		Message: "hello via url",
	}

	// Send multiple times — all should succeed since direct URL bypasses rate limiting.
	for i := 0; i < 3; i++ {
		result, _, err := srv.handleSendMessage(context.Background(), nil, input)
		if err != nil {
			t.Fatalf("unexpected error on send %d: %v", i+1, err)
		}
		if result.IsError {
			t.Fatalf("expected success on send %d, got error: %s", i+1, extractText(t, result))
		}
	}
}

// TestSendMessage_RateLimitBeforeClientResolution verifies that rate limit
// checking occurs before the client resolver makes an HTTP call to the agent.
// This ensures no outbound request is made when rate limited.
// Requirements: RLIM-5.3
func TestSendMessage_RateLimitBeforeClientResolution(t *testing.T) {
	// Atomic counter to track if the agent's HTTP handler was invoked.
	var agentCalls atomic.Int32
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agentCalls.Add(1)
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:        "task-no-call",
			ContextID: "ctx-no-call",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("ok")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("no-call-agent", agent.URL)
	// Set burst=1 with very low RPS.
	srv.rateLimiters.Set("no-call-agent", 0.001, 1)

	input := SendMessageInput{
		Agent:   "no-call-agent",
		Message: "hello",
	}

	// First send: consumes the single token, agent handler IS called.
	result1, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error on first send: %v", err)
	}
	if result1.IsError {
		t.Fatalf("expected first send to succeed, got error: %s", extractText(t, result1))
	}

	callsAfterFirst := agentCalls.Load()
	if callsAfterFirst == 0 {
		t.Fatal("expected agent handler to be called on first send")
	}

	// Second send: rate limited — agent handler should NOT be called.
	result2, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error on second send: %v", err)
	}
	if !result2.IsError {
		t.Fatal("expected second send to be rate limited")
	}

	callsAfterSecond := agentCalls.Load()
	if callsAfterSecond != callsAfterFirst {
		t.Errorf("expected no additional agent calls when rate limited; got %d calls total (expected %d)", callsAfterSecond, callsAfterFirst)
	}
}

// TestSendMessage_RateLimitViaToolCall verifies the full MCP tool call path
// for rate limited sends using the connect_agent + send_message tools.
// Requirements: RLIM-1.4, RLIM-2.1, RLIM-2.2
func TestSendMessage_RateLimitViaToolCall(t *testing.T) {
	// Mock A2A agent that serves both the agent card and JSON-RPC requests.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve agent card discovery request.
		if r.URL.Path == "/.well-known/agent.json" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		req, err := readJSONRPCRequest(r)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		msg := &a2a.Message{
			Role:      a2a.MessageRoleAgent,
			Parts:     a2a.ContentParts{a2a.NewTextPart("agent reply")},
			ContextID: "ctx-tool-1",
		}
		writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
	}))
	defer agent.Close()

	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	// Connect agent with burst=1 and very low RPS.
	connectResult := callTool(t, session, "connect_agent", map[string]any{
		"alias":            "tool-rl-agent",
		"agent_url":        agent.URL,
		"rate_limit_rps":   0.001,
		"rate_limit_burst": 1,
	})
	if connectResult.IsError {
		t.Fatalf("connect failed: %s", getTextContent(t, connectResult))
	}

	// First send: should succeed.
	sendResult1 := callTool(t, session, "send_message", map[string]any{
		"agent":   "tool-rl-agent",
		"message": "hello",
	})
	if sendResult1.IsError {
		t.Fatalf("expected first send to succeed, got error: %s", getTextContent(t, sendResult1))
	}

	// Second send: should be rate limited.
	sendResult2 := callTool(t, session, "send_message", map[string]any{
		"agent":   "tool-rl-agent",
		"message": "hello again",
	})
	if !sendResult2.IsError {
		t.Fatal("expected second send to be rate limited, got success")
	}

	text := getTextContent(t, sendResult2)
	if !strings.Contains(text, "rate limited") {
		t.Errorf("expected 'rate limited' in error, got: %s", text)
	}
	if !strings.Contains(text, "tool-rl-agent") {
		t.Errorf("expected alias in error, got: %s", text)
	}
}

// extractText is a test helper that extracts the text from the first TextContent item.
func extractText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("expected at least one content item, got none")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		// Try to marshal for debugging.
		data, _ := json.Marshal(result.Content[0])
		t.Fatalf("expected TextContent, got %T: %s", result.Content[0], string(data))
	}
	return tc.Text
}
