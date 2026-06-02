package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Feature: a2a-gateway-mcp, Property 8: Broadcast partial results attribution
// **Validates: Requirements AGMCP-12.2, AGMCP-12.3, AGMCP-12.5**

func TestPropertyBroadcastPartialResultsAttribution(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for valid alias names (lowercase letters only for simplicity).
	aliasGen := gen.RegexMatch(`[a-z]{3,8}`)

	// Generator for a set of valid aliases (1-5 registered agents).
	validAliasSetGen := gen.SliceOfN(5, aliasGen).SuchThat(func(v interface{}) bool {
		s := v.([]string)
		if len(s) == 0 {
			return false
		}
		// Ensure uniqueness.
		seen := make(map[string]bool)
		for _, a := range s {
			if a == "" || seen[a] {
				return false
			}
			seen[a] = true
		}
		return true
	})

	// Generator for a set of invalid aliases (1-5 unregistered aliases).
	invalidAliasSetGen := gen.SliceOfN(5, gen.RegexMatch(`invalid-[a-z]{3,6}`)).SuchThat(func(v interface{}) bool {
		s := v.([]string)
		if len(s) == 0 {
			return false
		}
		seen := make(map[string]bool)
		for _, a := range s {
			if a == "" || seen[a] {
				return false
			}
			seen[a] = true
		}
		return true
	})

	properties.Property("response contains exactly one entry per alias with correct status", prop.ForAll(
		func(validAliases []string, invalidAliases []string) bool {
			// Trim to ensure we stay within broadcast limits.
			if len(validAliases) > 10 {
				validAliases = validAliases[:10]
			}
			if len(invalidAliases) > 10 {
				invalidAliases = invalidAliases[:10]
			}

			// Ensure no overlap between valid and invalid aliases.
			validSet := make(map[string]bool)
			for _, a := range validAliases {
				validSet[a] = true
			}
			filteredInvalid := make([]string, 0)
			for _, a := range invalidAliases {
				if !validSet[a] {
					filteredInvalid = append(filteredInvalid, a)
				}
			}
			invalidAliases = filteredInvalid

			// Combine aliases (ensure total ≤ 20).
			allAliases := append(validAliases, invalidAliases...)
			if len(allAliases) == 0 || len(allAliases) > 20 {
				return true // skip degenerate cases
			}

			// Set up httptest.Server for valid agents returning JSON-RPC responses.
			agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				req, _ := readJSONRPCRequest(r)
				task := &a2a.Task{
					ID:     "task-broadcast",
					Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
					Artifacts: []*a2a.Artifact{
						{Parts: a2a.ContentParts{a2a.NewTextPart("broadcast response")}},
					},
				}
				writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
			}))
			defer agent.Close()

			// Create server and register valid aliases with AgentCard.
			srv := NewServer()
			for _, alias := range validAliases {
				srv.registry.Connect(alias, agent.URL, nil)
				srv.registry.SetCard(alias, &a2a.AgentCard{
					Name: alias,
					SupportedInterfaces: []*a2a.AgentInterface{
						a2a.NewAgentInterface(agent.URL, a2a.TransportProtocolJSONRPC),
					},
				})
			}
			// Invalid aliases are NOT registered.

			input := BroadcastMessageInput{
				Aliases: allAliases,
				Message: "hello broadcast",
			}

			result, _, err := srv.handleBroadcastMessage(context.Background(), nil, input)
			if err != nil {
				return false
			}
			if result.IsError {
				return false
			}

			// Parse the response JSON.
			textContent, ok := result.Content[0].(*mcp.TextContent)
			if !ok {
				return false
			}

			var results map[string]*broadcastResult
			if err := json.Unmarshal([]byte(textContent.Text), &results); err != nil {
				return false
			}

			// Verify exactly one entry per alias.
			if len(results) != len(allAliases) {
				return false
			}

			// Verify valid aliases have success status.
			for _, alias := range validAliases {
				r, exists := results[alias]
				if !exists {
					return false
				}
				if r.Status != "success" {
					return false
				}
			}

			// Verify invalid aliases have error status.
			for _, alias := range invalidAliases {
				r, exists := results[alias]
				if !exists {
					return false
				}
				if r.Status != "error" {
					return false
				}
				if r.Error == "" {
					return false
				}
			}

			return true
		},
		validAliasSetGen,
		invalidAliasSetGen,
	))

	properties.TestingRun(t)
}

// --- Unit Tests for broadcast_message handler (Task 9.3) ---

func TestHandleBroadcastMessage_AllSuccess(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:     "task-1",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("hello from agent")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := NewServer()
	for _, alias := range []string{"agent-a", "agent-b", "agent-c"} {
		srv.registry.Connect(alias, agent.URL, nil)
		srv.registry.SetCard(alias, &a2a.AgentCard{
			Name: alias,
			SupportedInterfaces: []*a2a.AgentInterface{
				a2a.NewAgentInterface(agent.URL, a2a.TransportProtocolJSONRPC),
			},
		})
	}

	input := BroadcastMessageInput{
		Aliases: []string{"agent-a", "agent-b", "agent-c"},
		Message: "broadcast test",
	}

	result, _, err := srv.handleBroadcastMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
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

	for _, alias := range []string{"agent-a", "agent-b", "agent-c"} {
		r, exists := results[alias]
		if !exists {
			t.Errorf("missing result for %s", alias)
			continue
		}
		if r.Status != "success" {
			t.Errorf("expected success for %s, got %s", alias, r.Status)
		}
		if r.Response != "hello from agent" {
			t.Errorf("expected response %q for %s, got %q", "hello from agent", alias, r.Response)
		}
	}
}

func TestHandleBroadcastMessage_AllFailure(t *testing.T) {
	// Agent returns a failed task via JSON-RPC.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID: "task-fail",
			Status: a2a.TaskStatus{
				State:   a2a.TaskStateFailed,
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("internal error")),
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := NewServer()
	for _, alias := range []string{"fail-a", "fail-b"} {
		srv.registry.Connect(alias, agent.URL, nil)
		srv.registry.SetCard(alias, &a2a.AgentCard{
			Name: alias,
			SupportedInterfaces: []*a2a.AgentInterface{
				a2a.NewAgentInterface(agent.URL, a2a.TransportProtocolJSONRPC),
			},
		})
	}

	input := BroadcastMessageInput{
		Aliases: []string{"fail-a", "fail-b"},
		Message: "broadcast test",
	}

	result, _, err := srv.handleBroadcastMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected non-error result (broadcast always succeeds at top level)")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}

	var results map[string]*broadcastResult
	if err := json.Unmarshal([]byte(textContent.Text), &results); err != nil {
		t.Fatalf("failed to parse results: %v", err)
	}

	for _, alias := range []string{"fail-a", "fail-b"} {
		r, exists := results[alias]
		if !exists {
			t.Errorf("missing result for %s", alias)
			continue
		}
		if r.Status != "error" {
			t.Errorf("expected error for %s, got %s", alias, r.Status)
		}
		if r.Error == "" {
			t.Errorf("expected non-empty error message for %s", alias)
		}
	}
}

func TestHandleBroadcastMessage_MixedResults(t *testing.T) {
	// Good agent returns completed task.
	goodAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:     "task-good",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("success response")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer goodAgent.Close()

	// Bad agent returns failed task.
	badAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID: "task-bad",
			Status: a2a.TaskStatus{
				State:   a2a.TaskStateFailed,
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("agent error")),
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer badAgent.Close()

	srv := NewServer()
	srv.registry.Connect("good-agent", goodAgent.URL, nil)
	srv.registry.SetCard("good-agent", &a2a.AgentCard{
		Name: "good-agent",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(goodAgent.URL, a2a.TransportProtocolJSONRPC),
		},
	})
	srv.registry.Connect("bad-agent", badAgent.URL, nil)
	srv.registry.SetCard("bad-agent", &a2a.AgentCard{
		Name: "bad-agent",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(badAgent.URL, a2a.TransportProtocolJSONRPC),
		},
	})

	input := BroadcastMessageInput{
		Aliases: []string{"good-agent", "bad-agent", "unregistered"},
		Message: "mixed test",
	}

	result, _, err := srv.handleBroadcastMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected non-error result")
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

	// Good agent should succeed.
	if results["good-agent"].Status != "success" {
		t.Errorf("expected success for good-agent, got %s", results["good-agent"].Status)
	}
	if results["good-agent"].Response != "success response" {
		t.Errorf("expected %q, got %q", "success response", results["good-agent"].Response)
	}

	// Bad agent should have error.
	if results["bad-agent"].Status != "error" {
		t.Errorf("expected error for bad-agent, got %s", results["bad-agent"].Status)
	}

	// Unregistered alias should have error.
	if results["unregistered"].Status != "error" {
		t.Errorf("expected error for unregistered, got %s", results["unregistered"].Status)
	}
}

func TestHandleBroadcastMessage_TimeoutEnforcement(t *testing.T) {
	// Agent with artificial delay that exceeds the timeout.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than the timeout.
		time.Sleep(3 * time.Second)
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:     "task-slow",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("should not see this")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := NewServer()
	srv.registry.Connect("slow-agent", agent.URL, nil)
	srv.registry.SetCard("slow-agent", &a2a.AgentCard{
		Name: "slow-agent",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(agent.URL, a2a.TransportProtocolJSONRPC),
		},
	})

	timeout := 1
	input := BroadcastMessageInput{
		Aliases:        []string{"slow-agent"},
		Message:        "timeout test",
		TimeoutSeconds: &timeout,
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

	r, exists := results["slow-agent"]
	if !exists {
		t.Fatal("missing result for slow-agent")
	}
	if r.Status != "error" {
		t.Errorf("expected error for slow-agent due to timeout, got %s", r.Status)
	}
}

func TestHandleBroadcastMessage_MaxAliasesValidation(t *testing.T) {
	srv := NewServer()

	// Create 21 aliases (exceeds max of 20).
	aliases := make([]string, 21)
	for i := range aliases {
		aliases[i] = "agent"
	}

	input := BroadcastMessageInput{
		Aliases: aliases,
		Message: "too many",
	}

	result, _, err := srv.handleBroadcastMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for >20 aliases")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text == "" {
		t.Error("expected non-empty error message")
	}
}

func TestHandleBroadcastMessage_EmptyAliases(t *testing.T) {
	srv := NewServer()

	input := BroadcastMessageInput{
		Aliases: []string{},
		Message: "empty aliases",
	}

	result, _, err := srv.handleBroadcastMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for empty aliases")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text == "" {
		t.Error("expected non-empty error message")
	}
}

func TestHandleBroadcastMessage_EmptyMessage(t *testing.T) {
	srv := NewServer()

	input := BroadcastMessageInput{
		Aliases: []string{"some-agent"},
		Message: "",
	}

	result, _, err := srv.handleBroadcastMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for empty message")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text == "" {
		t.Error("expected non-empty error message")
	}
}

func TestHandleBroadcastMessage_ConcurrentExecution(t *testing.T) {
	// Verify that broadcast executes concurrently by checking total time
	// is approximately the slowest agent, not the sum of all delays.
	var requestCount atomic.Int32

	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		// Each agent takes 200ms to respond.
		time.Sleep(200 * time.Millisecond)
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:     "task-concurrent",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("concurrent response")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := NewServer()
	numAgents := 5
	aliases := make([]string, numAgents)
	for i := range numAgents {
		alias := "concurrent-" + string(rune('a'+i))
		aliases[i] = alias
		srv.registry.Connect(alias, agent.URL, nil)
		srv.registry.SetCard(alias, &a2a.AgentCard{
			Name: alias,
			SupportedInterfaces: []*a2a.AgentInterface{
				a2a.NewAgentInterface(agent.URL, a2a.TransportProtocolJSONRPC),
			},
		})
	}

	input := BroadcastMessageInput{
		Aliases: aliases,
		Message: "concurrent test",
	}

	start := time.Now()
	result, _, err := srv.handleBroadcastMessage(context.Background(), nil, input)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error")
	}

	// If executed sequentially, it would take ~1000ms (5 * 200ms).
	// If concurrent, it should take ~200ms (plus some overhead).
	// Allow generous margin: should be less than 800ms.
	if elapsed > 800*time.Millisecond {
		t.Errorf("broadcast took %v, expected concurrent execution (~200ms), not sequential (~1000ms)", elapsed)
	}

	// Verify all agents were contacted.
	if count := requestCount.Load(); count != int32(numAgents) {
		t.Errorf("expected %d requests, got %d", numAgents, count)
	}

	// Verify all results are present.
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}

	var results map[string]*broadcastResult
	if err := json.Unmarshal([]byte(textContent.Text), &results); err != nil {
		t.Fatalf("failed to parse results: %v", err)
	}

	if len(results) != numAgents {
		t.Errorf("expected %d results, got %d", numAgents, len(results))
	}
}

func TestHandleBroadcastMessage_AuthRequired(t *testing.T) {
	// Agent returns auth-required with a status message.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:        "task-auth-broadcast",
			ContextID: "ctx-auth-broadcast",
			Status: a2a.TaskStatus{
				State:   a2a.TaskStateAuthRequired,
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("OAuth2 authentication required")),
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := NewServer()
	srv.registry.Connect("auth-broadcast-agent", agent.URL, nil)
	srv.registry.SetCard("auth-broadcast-agent", &a2a.AgentCard{
		Name: "auth-broadcast-agent",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(agent.URL, a2a.TransportProtocolJSONRPC),
		},
	})

	input := BroadcastMessageInput{
		Aliases: []string{"auth-broadcast-agent"},
		Message: "broadcast auth test",
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

	r, exists := results["auth-broadcast-agent"]
	if !exists {
		t.Fatal("missing result for auth-broadcast-agent")
	}
	if r.Status != "auth-required" {
		t.Errorf("expected status %q, got %q", "auth-required", r.Status)
	}
	if r.Response != "OAuth2 authentication required" {
		t.Errorf("expected response %q, got %q", "OAuth2 authentication required", r.Response)
	}
}

func TestHandleBroadcastMessage_MessageResponse(t *testing.T) {
	// Agent that returns a Message response (not a Task).
	messageAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		msg := &a2a.Message{
			Role:  a2a.MessageRoleAgent,
			Parts: a2a.ContentParts{a2a.NewTextPart("hello from message agent")},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
	}))
	defer messageAgent.Close()

	// Agent that returns a Task response (normal behavior).
	taskAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:     "task-1",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("hello from task agent")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer taskAgent.Close()

	srv := NewServer()
	srv.registry.Connect("msg-agent", messageAgent.URL, nil)
	srv.registry.SetCard("msg-agent", &a2a.AgentCard{
		Name: "msg-agent",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(messageAgent.URL, a2a.TransportProtocolJSONRPC),
		},
	})
	srv.registry.Connect("task-agent", taskAgent.URL, nil)
	srv.registry.SetCard("task-agent", &a2a.AgentCard{
		Name: "task-agent",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(taskAgent.URL, a2a.TransportProtocolJSONRPC),
		},
	})

	input := BroadcastMessageInput{
		Aliases: []string{"msg-agent", "task-agent"},
		Message: "broadcast with message response",
	}

	result, _, err := srv.handleBroadcastMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}

	var results map[string]*broadcastResult
	if err := json.Unmarshal([]byte(textContent.Text), &results); err != nil {
		t.Fatalf("failed to parse results: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Message agent should succeed with the message text.
	msgResult, exists := results["msg-agent"]
	if !exists {
		t.Fatal("missing result for msg-agent")
	}
	if msgResult.Status != "success" {
		t.Errorf("expected success for msg-agent, got %s", msgResult.Status)
	}
	if msgResult.Response != "hello from message agent" {
		t.Errorf("expected response %q for msg-agent, got %q", "hello from message agent", msgResult.Response)
	}

	// Task agent should also succeed with the task text.
	taskResult, exists := results["task-agent"]
	if !exists {
		t.Fatal("missing result for task-agent")
	}
	if taskResult.Status != "success" {
		t.Errorf("expected success for task-agent, got %s", taskResult.Status)
	}
	if taskResult.Response != "hello from task agent" {
		t.Errorf("expected response %q for task-agent, got %q", "hello from task agent", taskResult.Response)
	}
}

func TestHandleBroadcastMessage_MessageResponseNonTextParts(t *testing.T) {
	// Agent that returns a Message response with non-text parts.
	nonTextAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		msg := &a2a.Message{
			Role:  a2a.MessageRoleAgent,
			Parts: a2a.ContentParts{a2a.NewDataPart(map[string]any{"key": "value"})},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
	}))
	defer nonTextAgent.Close()

	srv := NewServer()
	srv.registry.Connect("nontext-agent", nonTextAgent.URL, nil)
	srv.registry.SetCard("nontext-agent", &a2a.AgentCard{
		Name: "nontext-agent",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(nonTextAgent.URL, a2a.TransportProtocolJSONRPC),
		},
	})

	input := BroadcastMessageInput{
		Aliases: []string{"nontext-agent"},
		Message: "broadcast with non-text message",
	}

	result, _, err := srv.handleBroadcastMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}

	var results map[string]*broadcastResult
	if err := json.Unmarshal([]byte(textContent.Text), &results); err != nil {
		t.Fatalf("failed to parse results: %v", err)
	}

	r, exists := results["nontext-agent"]
	if !exists {
		t.Fatal("missing result for nontext-agent")
	}
	if r.Status != "success" {
		t.Errorf("expected success for nontext-agent, got %s", r.Status)
	}
	if r.Response != "response contained non-text content that cannot be displayed" {
		t.Errorf("expected non-text content message, got %q", r.Response)
	}
}

func TestHandleBroadcastMessage_UnrecognizedResponse(t *testing.T) {
	// Agent that returns a JSON-RPC error (SDK will surface it as an error).
	weirdAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		writeJSONRPCError(w, req.ID, -32603, "internal error")
	}))
	defer weirdAgent.Close()

	srv := NewServer()
	srv.registry.Connect("weird-agent", weirdAgent.URL, nil)
	srv.registry.SetCard("weird-agent", &a2a.AgentCard{
		Name: "weird-agent",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(weirdAgent.URL, a2a.TransportProtocolJSONRPC),
		},
	})

	input := BroadcastMessageInput{
		Aliases: []string{"weird-agent"},
		Message: "broadcast with unrecognized response",
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

	r, exists := results["weird-agent"]
	if !exists {
		t.Fatal("missing result for weird-agent")
	}
	if r.Status != "error" {
		t.Errorf("expected error for weird-agent, got %s", r.Status)
	}
	if r.Error == "" {
		t.Error("expected non-empty error message for weird-agent")
	}
}
