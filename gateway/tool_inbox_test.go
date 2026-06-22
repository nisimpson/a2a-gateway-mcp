package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// pollForInboxEntries polls check_inbox until at least minEntries are present
// or the deadline elapses. Returns the parsed check_inbox output.
func pollForInboxEntries(t *testing.T, session *mcp.ClientSession, args map[string]any, minEntries int, deadline time.Duration) checkInboxResponse {
	t.Helper()
	ctx := context.Background()
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timer.C:
			t.Fatalf("timed out waiting for %d inbox entries", minEntries)
		case <-ticker.C:
			result, err := session.CallTool(ctx, &mcp.CallToolParams{
				Name:      "check_inbox",
				Arguments: args,
			})
			if err != nil {
				t.Fatalf("check_inbox error: %v", err)
			}
			text := getTextContent(t, result)
			var resp checkInboxResponse
			if err := json.Unmarshal([]byte(text), &resp); err != nil {
				t.Fatalf("failed to parse check_inbox response: %v", err)
			}
			if len(resp.Entries) >= minEntries {
				return resp
			}
		}
	}
}

type checkInboxResponse struct {
	Entries []inboxSummary `json:"entries"`
}

type inboxSummary struct {
	Alias     string `json:"alias"`
	TaskID    string `json:"task_id,omitempty"`
	ContextID string `json:"context_id,omitempty"`
	State     string `json:"state"`
	Timestamp string `json:"timestamp"`
}

type readInboxResponse struct {
	Messages []json.RawMessage `json:"messages"`
}

type asyncSendResponse struct {
	Async struct {
		Alias  string `json:"alias"`
		Status string `json:"status"`
	} `json:"async"`
}

// mockA2AAgentServer creates a test HTTP server that responds to JSON-RPC
// sendMessage requests with a Message response.
func mockA2AAgentServer(t *testing.T, contextID string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, err := readJSONRPCRequest(r)
		if err != nil {
			writeJSONRPCError(w, "", -32600, "invalid request")
			return
		}

		// Return a Message response (simpler structure, avoids schema
		// validation issues with nested Task artifacts).
		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("hello from agent"))
		msg.ContextID = contextID

		writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
	}))
}

// TestInbox_AsyncSendCheckRead tests the full round-trip:
// async send → check_inbox → read_inbox → check_inbox (empty).
// Validates: AINB-1.2, AINB-1.4, AINB-3.1, AINB-4.1
func TestInbox_AsyncSendCheckRead(t *testing.T) {
	agentServer := mockA2AAgentServer(t, "ctx-456")
	defer agentServer.Close()

	srv := newTestServerWithAgent("test-agent", agentServer.URL)
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	ctx := context.Background()

	// 1. Send async message.
	result := callTool(t, session, "send_message", map[string]any{
		"agent":   "test-agent",
		"message": "hello",
		"async":   true,
	})
	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	// Verify async response format.
	text := getTextContent(t, result)
	var asyncResp asyncSendResponse
	if err := json.Unmarshal([]byte(text), &asyncResp); err != nil {
		t.Fatalf("failed to parse async response: %v", err)
	}
	if asyncResp.Async.Alias != "test-agent" {
		t.Errorf("expected alias %q, got %q", "test-agent", asyncResp.Async.Alias)
	}
	if asyncResp.Async.Status != "dispatched" {
		t.Errorf("expected status %q, got %q", "dispatched", asyncResp.Async.Status)
	}

	// 2. Poll check_inbox until the entry appears.
	checkResp := pollForInboxEntries(t, session, map[string]any{}, 1, 3*time.Second)

	if len(checkResp.Entries) != 1 {
		t.Fatalf("expected 1 inbox entry, got %d", len(checkResp.Entries))
	}
	entry := checkResp.Entries[0]
	if entry.Alias != "test-agent" {
		t.Errorf("expected alias %q, got %q", "test-agent", entry.Alias)
	}
	if entry.ContextID != "ctx-456" {
		t.Errorf("expected context_id %q, got %q", "ctx-456", entry.ContextID)
	}
	if entry.State != "completed" {
		t.Errorf("expected state %q, got %q", "completed", entry.State)
	}

	// 3. Read inbox — destructive read.
	readResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "read_inbox",
		Arguments: map[string]any{"alias": "test-agent"},
	})
	if err != nil {
		t.Fatalf("read_inbox error: %v", err)
	}
	if readResult.IsError {
		t.Fatalf("read_inbox returned error: %s", getTextContent(t, readResult))
	}

	readText := getTextContent(t, readResult)
	var readResp readInboxResponse
	if err := json.Unmarshal([]byte(readText), &readResp); err != nil {
		t.Fatalf("failed to parse read_inbox response: %v", err)
	}
	if len(readResp.Messages) != 1 {
		t.Fatalf("expected 1 message in read_inbox, got %d", len(readResp.Messages))
	}

	// 4. Check inbox again — should be empty after read.
	checkResult2, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "check_inbox",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("check_inbox error: %v", err)
	}
	text2 := getTextContent(t, checkResult2)
	var checkResp2 checkInboxResponse
	if err := json.Unmarshal([]byte(text2), &checkResp2); err != nil {
		t.Fatalf("failed to parse check_inbox response: %v", err)
	}
	if len(checkResp2.Entries) != 0 {
		t.Errorf("expected 0 entries after read_inbox, got %d", len(checkResp2.Entries))
	}
}

// TestInbox_AsyncBroadcastPerAgentEntries tests that async broadcast deposits
// entries for each agent independently.
// Validates: AINB-2.3
func TestInbox_AsyncBroadcastPerAgentEntries(t *testing.T) {
	agent1 := mockA2AAgentServer(t, "ctx-a1")
	defer agent1.Close()

	agent2 := mockA2AAgentServer(t, "ctx-a2")
	defer agent2.Close()

	srv := newTestServerWithAgent("agent-1", agent1.URL)
	srv.registry.Connect("agent-2", agent2.URL, nil, "")
	srv.registry.SetCard("agent-2", &a2a.AgentCard{
		Name: "agent-2",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(agent2.URL, a2a.TransportProtocolJSONRPC),
		},
	})

	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	// Async broadcast to both agents.
	result := callTool(t, session, "broadcast_message", map[string]any{
		"aliases": []any{"agent-1", "agent-2"},
		"message": "hello broadcast",
		"async":   true,
	})
	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	// Poll until we get entries from both agents.
	checkResp := pollForInboxEntries(t, session, map[string]any{}, 2, 3*time.Second)

	if len(checkResp.Entries) < 2 {
		t.Fatalf("expected at least 2 inbox entries, got %d", len(checkResp.Entries))
	}

	// Verify entries from both agents are present.
	aliases := make(map[string]bool)
	for _, e := range checkResp.Entries {
		aliases[e.Alias] = true
	}
	if !aliases["agent-1"] {
		t.Error("expected inbox entry from agent-1")
	}
	if !aliases["agent-2"] {
		t.Error("expected inbox entry from agent-2")
	}
}

// TestInbox_SyncSendDoesNotDeposit verifies that synchronous send_message
// does NOT place entries into the inbox.
// Validates: AINB-5.5
func TestInbox_SyncSendDoesNotDeposit(t *testing.T) {
	agentServer := mockA2AAgentServer(t, "ctx-sync")
	defer agentServer.Close()

	srv := newTestServerWithAgent("sync-agent", agentServer.URL)
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	ctx := context.Background()

	// Send synchronously (async not set).
	result := callTool(t, session, "send_message", map[string]any{
		"agent":   "sync-agent",
		"message": "hello sync",
	})
	if result.IsError {
		t.Fatalf("expected success for sync send, got error: %s", getTextContent(t, result))
	}

	// Give a brief moment for any hypothetical background work.
	time.Sleep(200 * time.Millisecond)

	// Check inbox — should be empty.
	checkResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "check_inbox",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("check_inbox error: %v", err)
	}
	text := getTextContent(t, checkResult)
	var checkResp checkInboxResponse
	if err := json.Unmarshal([]byte(text), &checkResp); err != nil {
		t.Fatalf("failed to parse check_inbox response: %v", err)
	}
	if len(checkResp.Entries) != 0 {
		t.Errorf("expected 0 entries in inbox after sync send, got %d", len(checkResp.Entries))
	}
}

// TestInbox_TTLExpiration verifies that inbox entries expire after the configured TTL.
// Validates: AINB-5.2
func TestInbox_TTLExpiration(t *testing.T) {
	agentServer := mockA2AAgentServer(t, "ctx-ttl")
	defer agentServer.Close()

	// Create server with short TTL (500ms) — long enough to observe the entry,
	// short enough to expire within the test.
	srv := NewServer(WithInboxTTL(500 * time.Millisecond))
	srv.registry.Connect("ttl-agent", agentServer.URL, nil, "")
	srv.registry.SetCard("ttl-agent", &a2a.AgentCard{
		Name: "ttl-agent",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(agentServer.URL, a2a.TransportProtocolJSONRPC),
		},
	})

	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	ctx := context.Background()

	// Send async message.
	result := callTool(t, session, "send_message", map[string]any{
		"agent":   "ttl-agent",
		"message": "hello ttl",
		"async":   true,
	})
	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	// Wait for entry to appear.
	pollForInboxEntries(t, session, map[string]any{}, 1, 3*time.Second)

	// Wait for TTL to expire (500ms TTL + buffer).
	time.Sleep(700 * time.Millisecond)

	// Check inbox — entries should have expired.
	checkResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "check_inbox",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("check_inbox error: %v", err)
	}
	text := getTextContent(t, checkResult)
	var checkResp checkInboxResponse
	if err := json.Unmarshal([]byte(text), &checkResp); err != nil {
		t.Fatalf("failed to parse check_inbox response: %v", err)
	}
	if len(checkResp.Entries) != 0 {
		t.Errorf("expected 0 entries after TTL expiration, got %d", len(checkResp.Entries))
	}
}
