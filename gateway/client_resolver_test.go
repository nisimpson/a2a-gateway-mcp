package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestClientResolver_Evict_RemovesCachedClient(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer agent.Close()

	registry := NewAgentRegistry()
	registry.Connect("test-agent", agent.URL, nil)
	registry.SetCard("test-agent", &a2a.AgentCard{
		Name: "test-agent",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(agent.URL, a2a.TransportProtocolJSONRPC),
		},
	})

	resolver := newClientResolver(registry, &http.Client{})
	resolved := &ResolveResult{URL: agent.URL, IsAlias: true, Alias: "test-agent"}

	// First resolve — creates and caches the client.
	_, err := resolver.Resolve(context.Background(), resolved)
	if err != nil {
		t.Fatalf("unexpected error on first resolve: %v", err)
	}

	// Verify client is cached.
	resolver.mu.RLock()
	_, cached := resolver.clients[agent.URL]
	resolver.mu.RUnlock()
	if !cached {
		t.Fatal("expected client to be cached after first resolve")
	}

	// Evict the client.
	resolver.Evict(agent.URL)

	// Verify client is no longer cached.
	resolver.mu.RLock()
	_, cached = resolver.clients[agent.URL]
	resolver.mu.RUnlock()
	if cached {
		t.Fatal("expected client to be evicted from cache")
	}
}

func TestClientResolver_Evict_NonExistentURL_NoOp(t *testing.T) {
	registry := NewAgentRegistry()
	resolver := newClientResolver(registry, &http.Client{})

	// Evicting a non-existent URL should not panic or error.
	resolver.Evict("http://nonexistent.example.com")
}

func TestClientResolver_CreateClient_JSONRPCCard_NoRESTOption(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer agent.Close()

	registry := NewAgentRegistry()
	resolver := newClientResolver(registry, &http.Client{})

	card := &a2a.AgentCard{
		Name: "jsonrpc-agent",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(agent.URL, a2a.TransportProtocolJSONRPC),
		},
	}
	resolved := &ResolveResult{URL: agent.URL, IsAlias: true, Alias: "jsonrpc-agent"}

	// Create client with JSON-RPC card — should succeed without error.
	client, err := resolver.createClient(context.Background(), resolved, card, &http.Client{})
	if err != nil {
		t.Fatalf("unexpected error creating client from JSON-RPC card: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestClientResolver_CreateClient_RESTCard(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer agent.Close()

	registry := NewAgentRegistry()
	resolver := newClientResolver(registry, &http.Client{})

	card := &a2a.AgentCard{
		Name: "rest-agent",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(agent.URL, a2a.TransportProtocolHTTPJSON),
		},
	}
	resolved := &ResolveResult{URL: agent.URL, IsAlias: true, Alias: "rest-agent"}

	// Create client with REST card — should succeed without error.
	client, err := resolver.createClient(context.Background(), resolved, card, &http.Client{})
	if err != nil {
		t.Fatalf("unexpected error creating client from REST card: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestClientResolver_CreateClient_NilCard_FallsBackToJSONRPC(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer agent.Close()

	registry := NewAgentRegistry()
	resolver := newClientResolver(registry, &http.Client{})
	resolved := &ResolveResult{URL: agent.URL, IsAlias: true, Alias: "no-card-agent"}

	// Create client with nil card — should fall back to JSON-RPC.
	client, err := resolver.createClient(context.Background(), resolved, nil, &http.Client{})
	if err != nil {
		t.Fatalf("unexpected error creating client with nil card: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestHandleDisconnectAgent_EvictsCachedClient(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:     "task-1",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("hello")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("evict-agent", agent.URL)

	// Send a message to cache the client.
	input := SendMessageInput{Agent: "evict-agent", Message: "hello"}
	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error")
	}

	// Verify client is cached.
	srv.clients.mu.RLock()
	_, cached := srv.clients.clients[agent.URL]
	srv.clients.mu.RUnlock()
	if !cached {
		t.Fatal("expected client to be cached")
	}

	// Disconnect the agent.
	disconnectInput := DisconnectAgentInput{Alias: "evict-agent"}
	disconnectResult, _, err := srv.handleDisconnectAgent(context.Background(), nil, disconnectInput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if disconnectResult.IsError {
		t.Fatalf("expected success, got error: %v", disconnectResult.Content)
	}

	// Verify client was evicted.
	srv.clients.mu.RLock()
	_, cached = srv.clients.clients[agent.URL]
	srv.clients.mu.RUnlock()
	if cached {
		t.Fatal("expected client to be evicted after disconnect")
	}
}

func TestHandleTaskResult_UnrecognizedState_ReturnsProtocolError(t *testing.T) {
	srv := NewServer()

	// Simulate a v0.x task state.
	task := &a2a.Task{
		ID:     "task-1",
		Status: a2a.TaskStatus{State: a2a.TaskState("input-required")}, // v0.x format
	}
	resolved := &ResolveResult{IsAlias: false, URL: "http://example.com"}

	result, _, err := srv.handleTaskResult(context.Background(), nil, task, resolved, "", srv.pollTimeout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for unrecognized state")
	}

	textContent := result.Content[0].(*mcp.TextContent)
	if !contains(textContent.Text, "input-required") {
		t.Errorf("expected error to contain state value, got: %q", textContent.Text)
	}
	if !contains(textContent.Text, "A2A protocol v1.0 or later") {
		t.Errorf("expected error to contain version hint, got: %q", textContent.Text)
	}
}

func TestHandleTaskResult_UnrecognizedState_WithID_NoPolling(t *testing.T) {
	srv := NewServer()

	// v0.x task with an ID — should NOT poll, should return immediate error.
	task := &a2a.Task{
		ID:     "task-with-id",
		Status: a2a.TaskStatus{State: a2a.TaskState("working")}, // v0.x lowercase
	}
	resolved := &ResolveResult{IsAlias: false, URL: "http://example.com"}

	result, _, err := srv.handleTaskResult(context.Background(), nil, task, resolved, "", srv.pollTimeout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for unrecognized state")
	}

	textContent := result.Content[0].(*mcp.TextContent)
	if !contains(textContent.Text, "working") {
		t.Errorf("expected error to contain state value, got: %q", textContent.Text)
	}
	if !contains(textContent.Text, "A2A protocol v1.0 or later") {
		t.Errorf("expected error to contain version hint, got: %q", textContent.Text)
	}
}

func TestFormatStreamTask_UnrecognizedState_ReturnsProtocolError(t *testing.T) {
	srv := NewServer()

	task := &a2a.Task{
		ID:     "task-1",
		Status: a2a.TaskStatus{State: a2a.TaskState("completed")}, // v0.x lowercase
	}

	result := srv.formatStreamTask(task)
	if !result.IsError {
		t.Fatal("expected error result for unrecognized state in streaming path")
	}

	textContent := result.Content[0].(*mcp.TextContent)
	if !contains(textContent.Text, "completed") {
		t.Errorf("expected error to contain state value, got: %q", textContent.Text)
	}
	if !contains(textContent.Text, "A2A protocol v1.0 or later") {
		t.Errorf("expected error to contain version hint, got: %q", textContent.Text)
	}
}

func TestHandleConnectAgent_EvictsCachedClientOnReconnect(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve agent card at /.well-known/agent.json
		if r.URL.Path == "/.well-known/agent.json" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:     "task-1",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("hello")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := NewServer()

	// First connect.
	connectInput := ConnectAgentInput{Alias: "reconnect-agent", AgentURL: agent.URL}
	_, _, err := srv.handleConnectAgent(context.Background(), nil, connectInput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Send a message to cache the client.
	sendInput := SendMessageInput{Agent: "reconnect-agent", Message: "hello"}
	result, _, err := srv.handleSendMessage(context.Background(), nil, sendInput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error")
	}

	// Verify client is cached.
	srv.clients.mu.RLock()
	_, cached := srv.clients.clients[agent.URL]
	srv.clients.mu.RUnlock()
	if !cached {
		t.Fatal("expected client to be cached")
	}

	// Reconnect with same URL (simulating card/header change).
	_, _, err = srv.handleConnectAgent(context.Background(), nil, connectInput)
	if err != nil {
		t.Fatalf("unexpected error on reconnect: %v", err)
	}

	// Verify client was evicted by reconnect.
	srv.clients.mu.RLock()
	_, cached = srv.clients.clients[agent.URL]
	srv.clients.mu.RUnlock()
	if cached {
		t.Fatal("expected client to be evicted after reconnect")
	}
}

func TestHandleTaskResult_KnownNonTerminal_WorkingState_Polls(t *testing.T) {
	// Agent returns "working" on first call (SendMessage), then "completed" on poll (GetTask).
	var callCount int
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		callCount++
		if callCount == 1 {
			task := &a2a.Task{
				ID:     "task-poll",
				Status: a2a.TaskStatus{State: a2a.TaskStateWorking},
			}
			writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
		} else {
			task := &a2a.Task{
				ID:     "task-poll",
				Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
				Artifacts: []*a2a.Artifact{
					{Parts: a2a.ContentParts{a2a.NewTextPart("done")}},
				},
			}
			writeJSONRPCResult(w, req.ID, task)
		}
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("poll-agent", agent.URL)

	input := SendMessageInput{Agent: "poll-agent", Message: "hello"}
	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success after polling, got error: %v", result.Content)
	}

	// Verify polling occurred.
	if callCount < 2 {
		t.Errorf("expected at least 2 HTTP calls (send + poll), got %d", callCount)
	}
}

func TestHandleConnectAgent_EvictsOnURLChange(t *testing.T) {
	agent1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent.json" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:     "task-1",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("from agent1")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent1.Close()

	agent2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent.json" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:     "task-2",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("from agent2")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent2.Close()

	srv := NewServer()

	// Connect to agent1.
	connectInput := ConnectAgentInput{Alias: "url-change-agent", AgentURL: agent1.URL}
	_, _, _ = srv.handleConnectAgent(context.Background(), nil, connectInput)

	// Send message to cache client for agent1.
	sendInput := SendMessageInput{Agent: "url-change-agent", Message: "hello"}
	_, _, _ = srv.handleSendMessage(context.Background(), nil, sendInput)

	// Verify agent1 client is cached.
	srv.clients.mu.RLock()
	_, cached := srv.clients.clients[agent1.URL]
	srv.clients.mu.RUnlock()
	if !cached {
		t.Fatal("expected agent1 client to be cached")
	}

	// Reconnect to agent2 (different URL).
	connectInput2 := ConnectAgentInput{Alias: "url-change-agent", AgentURL: agent2.URL}
	_, _, _ = srv.handleConnectAgent(context.Background(), nil, connectInput2)

	// Verify agent1 client was evicted.
	srv.clients.mu.RLock()
	_, cached = srv.clients.clients[agent1.URL]
	srv.clients.mu.RUnlock()
	if cached {
		t.Fatal("expected agent1 client to be evicted after URL change")
	}
}

func TestCreateClient_JSONRPCCard_SendsJSONRPCFormat(t *testing.T) {
	// Integration test: verify that a client created from a JSON-RPC card
	// actually sends JSON-RPC formatted requests (with "jsonrpc" field).
	var receivedBody []byte
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = body
		// Return a valid JSON-RPC response.
		task := &a2a.Task{
			ID:     "task-1",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("ok")}},
			},
		}
		writeJSONRPCResult(w, "1", jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := NewServer()
	srv.registry.Connect("jsonrpc-test", agent.URL, nil)
	srv.registry.SetCard("jsonrpc-test", &a2a.AgentCard{
		Name: "jsonrpc-test",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(agent.URL, a2a.TransportProtocolJSONRPC),
		},
	})

	input := SendMessageInput{Agent: "jsonrpc-test", Message: "test"}
	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Verify the request body contains "jsonrpc" (JSON-RPC envelope).
	if !contains(string(receivedBody), "jsonrpc") {
		t.Errorf("expected JSON-RPC formatted request body containing 'jsonrpc', got: %s", string(receivedBody))
	}
}
