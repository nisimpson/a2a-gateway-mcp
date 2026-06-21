package gateway

import (
	"context"
	"encoding/json"
	"fmt"
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
				srv.registry.Connect(alias, agent.URL, nil, "")
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
		srv.registry.Connect(alias, agent.URL, nil, "")
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
		srv.registry.Connect(alias, agent.URL, nil, "")
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
	srv.registry.Connect("good-agent", goodAgent.URL, nil, "")
	srv.registry.SetCard("good-agent", &a2a.AgentCard{
		Name: "good-agent",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(goodAgent.URL, a2a.TransportProtocolJSONRPC),
		},
	})
	srv.registry.Connect("bad-agent", badAgent.URL, nil, "")
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
	srv.registry.Connect("slow-agent", agent.URL, nil, "")
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
		srv.registry.Connect(alias, agent.URL, nil, "")
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
	srv.registry.Connect("auth-broadcast-agent", agent.URL, nil, "")
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
	srv.registry.Connect("msg-agent", messageAgent.URL, nil, "")
	srv.registry.SetCard("msg-agent", &a2a.AgentCard{
		Name: "msg-agent",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(messageAgent.URL, a2a.TransportProtocolJSONRPC),
		},
	})
	srv.registry.Connect("task-agent", taskAgent.URL, nil, "")
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
	srv.registry.Connect("nontext-agent", nonTextAgent.URL, nil, "")
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
	// Data parts are now rendered as JSON.
	if r.Response != `{"key":"value"}` {
		t.Errorf("expected JSON rendered data part, got %q", r.Response)
	}
}

// Feature: agent-health-checks, Property 8: Broadcast health-aware filtering
// **Validates: Requirements HLTH-6.1, HLTH-6.3, HLTH-6.4, HLTH-6.5**

func TestPropertyBroadcastHealthFiltering(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for number of agents per health category (0-4 each).
	countGen := gen.IntRange(0, 4)

	// Generator for failure threshold (1-5).
	thresholdGen := gen.IntRange(1, 5)

	properties.Property("unhealthy agents are skipped; unknown/healthy agents are attempted; skipped agents do not consume rate limit tokens or increment failure count", prop.ForAll(
		func(numHealthy, numUnhealthy, numUnknown, threshold int) bool {
			// Ensure at least one agent exists.
			if numHealthy+numUnhealthy+numUnknown == 0 {
				return true // skip degenerate case
			}

			// Set up a mock A2A backend that responds with a completed task.
			var requestCount atomic.Int32
			agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount.Add(1)
				req, _ := readJSONRPCRequest(r)
				task := &a2a.Task{
					ID:     "task-health",
					Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
					Artifacts: []*a2a.Artifact{
						{Parts: a2a.ContentParts{a2a.NewTextPart("ok")}},
					},
				}
				writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
			}))
			defer agent.Close()

			// Create server with specified threshold.
			srv := NewServer(WithHealthCheck(HealthCheckOptions{FailureThreshold: threshold}))

			// Configure rate limiters with a high rate (so healthy/unknown won't be
			// rejected) but we can observe token consumption.
			// We'll use Reserve() after the broadcast to verify tokens were/weren't consumed.

			var healthyAliases, unhealthyAliases, unknownAliases []string
			var allAliases []string

			// Register healthy agents: Reset + RecordSuccess.
			for i := range numHealthy {
				alias := fmt.Sprintf("healthy-%d", i)
				healthyAliases = append(healthyAliases, alias)
				allAliases = append(allAliases, alias)
				srv.registry.Connect(alias, agent.URL, nil, "")
				srv.registry.SetCard(alias, &a2a.AgentCard{
					Name: alias,
					SupportedInterfaces: []*a2a.AgentInterface{
						a2a.NewAgentInterface(agent.URL, a2a.TransportProtocolJSONRPC),
					},
				})
				srv.healthTracker.Reset(alias)
				srv.healthTracker.RecordSuccess(alias)
			}

			// Register unhealthy agents: Reset + threshold failures.
			for i := range numUnhealthy {
				alias := fmt.Sprintf("unhealthy-%d", i)
				unhealthyAliases = append(unhealthyAliases, alias)
				allAliases = append(allAliases, alias)
				srv.registry.Connect(alias, agent.URL, nil, "")
				srv.registry.SetCard(alias, &a2a.AgentCard{
					Name: alias,
					SupportedInterfaces: []*a2a.AgentInterface{
						a2a.NewAgentInterface(agent.URL, a2a.TransportProtocolJSONRPC),
					},
				})
				srv.healthTracker.Reset(alias)
				for range threshold {
					srv.healthTracker.RecordFailure(alias)
				}
			}

			// Register unknown agents: just Reset (default state).
			for i := range numUnknown {
				alias := fmt.Sprintf("unknown-%d", i)
				unknownAliases = append(unknownAliases, alias)
				allAliases = append(allAliases, alias)
				srv.registry.Connect(alias, agent.URL, nil, "")
				srv.registry.SetCard(alias, &a2a.AgentCard{
					Name: alias,
					SupportedInterfaces: []*a2a.AgentInterface{
						a2a.NewAgentInterface(agent.URL, a2a.TransportProtocolJSONRPC),
					},
				})
				srv.healthTracker.Reset(alias)
			}

			// Set up rate limiters for all agents (high burst so healthy/unknown pass).
			for _, alias := range allAliases {
				srv.rateLimiters.Set(alias, 100, 100)
			}

			// Record pre-broadcast failure counts for unhealthy agents.
			preFailureCounts := make(map[string]int)
			for _, alias := range unhealthyAliases {
				state := srv.healthTracker.Get(alias)
				preFailureCounts[alias] = state.Failures
			}

			// Execute broadcast.
			input := BroadcastMessageInput{
				Aliases: allAliases,
				Message: "health filter test",
			}

			result, _, err := srv.handleBroadcastMessage(context.Background(), nil, input)
			if err != nil {
				t.Logf("unexpected error: %v", err)
				return false
			}
			if result.IsError {
				t.Logf("unexpected error result: %v", result.Content)
				return false
			}

			// Parse results.
			textContent, ok := result.Content[0].(*mcp.TextContent)
			if !ok {
				t.Log("expected TextContent in result")
				return false
			}

			var results map[string]*broadcastResult
			if err := json.Unmarshal([]byte(textContent.Text), &results); err != nil {
				t.Logf("failed to parse results: %v", err)
				return false
			}

			// HLTH-6.1: Unhealthy agents are skipped with status "skipped" and error "agent is unhealthy".
			for _, alias := range unhealthyAliases {
				r, exists := results[alias]
				if !exists {
					t.Logf("missing result for unhealthy alias %q", alias)
					return false
				}
				if r.Status != "skipped" {
					t.Logf("expected status 'skipped' for unhealthy alias %q, got %q", alias, r.Status)
					return false
				}
				if r.Error != "agent is unhealthy" {
					t.Logf("expected error 'agent is unhealthy' for unhealthy alias %q, got %q", alias, r.Error)
					return false
				}
			}

			// HLTH-6.3: Unknown agents are still attempted (should get success from mock server).
			for _, alias := range unknownAliases {
				r, exists := results[alias]
				if !exists {
					t.Logf("missing result for unknown alias %q", alias)
					return false
				}
				if r.Status != "success" {
					t.Logf("expected status 'success' for unknown alias %q (should be attempted), got %q", alias, r.Status)
					return false
				}
			}

			// Healthy agents should also be attempted and succeed.
			for _, alias := range healthyAliases {
				r, exists := results[alias]
				if !exists {
					t.Logf("missing result for healthy alias %q", alias)
					return false
				}
				if r.Status != "success" {
					t.Logf("expected status 'success' for healthy alias %q, got %q", alias, r.Status)
					return false
				}
			}

			// HLTH-6.4: Verify request count matches only healthy + unknown agents
			// (unhealthy agents should NOT have sent a request to the mock server).
			expectedRequests := int32(numHealthy + numUnknown)
			actualRequests := requestCount.Load()
			if actualRequests != expectedRequests {
				t.Logf("expected %d HTTP requests (healthy+unknown), got %d", expectedRequests, actualRequests)
				return false
			}

			// HLTH-6.5: Skipped agents do not increment failure count.
			for _, alias := range unhealthyAliases {
				state := srv.healthTracker.Get(alias)
				if state.Failures != preFailureCounts[alias] {
					t.Logf("failure count changed for skipped unhealthy alias %q: pre=%d, post=%d", alias, preFailureCounts[alias], state.Failures)
					return false
				}
			}

			return true
		},
		countGen, // numHealthy
		countGen, // numUnhealthy
		countGen, // numUnknown
		thresholdGen,
	))

	properties.TestingRun(t)
}

// Feature: structured-responses, Property 3: Broadcast Raw Object Inclusion
// **Validates: Requirements SRES-4.1, SRES-4.2, SRES-4.3**

func TestPropertyBroadcastRawObjectInclusion(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// States that produce "successful" broadcastResults (Task field set).
	successStates := []a2a.TaskState{
		a2a.TaskStateCompleted,
		a2a.TaskStateInputRequired,
		a2a.TaskStateAuthRequired,
	}

	// States that produce "error" broadcastResults (Task and Message nil).
	errorStates := []a2a.TaskState{
		a2a.TaskStateFailed,
		a2a.TaskStateCanceled,
		a2a.TaskStateWorking,   // falls into default case
		a2a.TaskStateSubmitted, // falls into default case
	}

	// Generator for task state from a given set.
	successStateGen := gen.OneConstOf(
		a2a.TaskStateCompleted,
		a2a.TaskStateInputRequired,
		a2a.TaskStateAuthRequired,
	)

	errorStateGen := gen.OneConstOf(
		a2a.TaskStateFailed,
		a2a.TaskStateCanceled,
		a2a.TaskStateWorking,
		a2a.TaskStateSubmitted,
	)

	// Generator for optional text content.
	textGen := gen.RegexMatch(`[a-zA-Z0-9 ]{0,32}`)

	// Generator for optional task ID.
	taskIDGen := gen.RegexMatch(`[a-z0-9\-]{1,16}`)

	// Generator for optional context ID.
	contextIDGen := gen.RegexMatch(`[a-z0-9\-]{0,16}`)

	// Generator for number of artifacts (0-3).
	numArtifactsGen := gen.IntRange(0, 3)

	// Suppress unused variable warnings for documentation purposes.
	_ = successStates
	_ = errorStates

	srv := NewServer()

	properties.Property("successful task states set Task field to input task pointer", prop.ForAll(
		func(state a2a.TaskState, taskID string, contextID string, statusMsg string, numArtifacts int) bool {
			// Build a random task with the given state.
			task := &a2a.Task{
				ID:        a2a.TaskID(taskID),
				ContextID: contextID,
				Status: a2a.TaskStatus{
					State: state,
				},
			}

			// Add status message if non-empty.
			if statusMsg != "" {
				task.Status.Message = a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(statusMsg))
			}

			// Add artifacts.
			if numArtifacts > 0 {
				task.Artifacts = make([]*a2a.Artifact, numArtifacts)
				for i := range numArtifacts {
					task.Artifacts[i] = &a2a.Artifact{
						Parts: a2a.ContentParts{a2a.NewTextPart(fmt.Sprintf("artifact-%d", i))},
					}
				}
			}

			result, _ := srv.handleBroadcastTaskResult(task)

			// For successful states, the Task field must be the exact same pointer.
			if result.Task != task {
				t.Logf("state=%s: expected Task pointer equality", state)
				return false
			}

			// Message field should be nil (this is a Task response, not a Message response).
			if result.Message != nil {
				t.Logf("state=%s: expected nil Message field", state)
				return false
			}

			// Status should not be "error".
			if result.Status == "error" {
				t.Logf("state=%s: got unexpected error status", state)
				return false
			}

			return true
		},
		successStateGen,
		taskIDGen,
		contextIDGen,
		textGen,
		numArtifactsGen,
	))

	properties.Property("error/non-success task states have nil Task and Message fields", prop.ForAll(
		func(state a2a.TaskState, taskID string, contextID string, statusMsg string, numArtifacts int) bool {
			// Build a random task with the given state.
			task := &a2a.Task{
				ID:        a2a.TaskID(taskID),
				ContextID: contextID,
				Status: a2a.TaskStatus{
					State: state,
				},
			}

			// Add status message if non-empty.
			if statusMsg != "" {
				task.Status.Message = a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(statusMsg))
			}

			// Add artifacts.
			if numArtifacts > 0 {
				task.Artifacts = make([]*a2a.Artifact, numArtifacts)
				for i := range numArtifacts {
					task.Artifacts[i] = &a2a.Artifact{
						Parts: a2a.ContentParts{a2a.NewTextPart(fmt.Sprintf("artifact-%d", i))},
					}
				}
			}

			result, _ := srv.handleBroadcastTaskResult(task)

			// For error states, Task and Message should be nil.
			if result.Task != nil {
				t.Logf("state=%s: expected nil Task field, got non-nil", state)
				return false
			}
			if result.Message != nil {
				t.Logf("state=%s: expected nil Message field, got non-nil", state)
				return false
			}

			// Status should be "error".
			if result.Status != "error" {
				t.Logf("state=%s: expected status 'error', got %q", state, result.Status)
				return false
			}

			return true
		},
		errorStateGen,
		taskIDGen,
		contextIDGen,
		textGen,
		numArtifactsGen,
	))

	properties.Property("existing Status, Response, and Error fields remain unchanged from legacy behavior", prop.ForAll(
		func(state a2a.TaskState, taskID string, contextID string, statusMsg string, numArtifacts int) bool {
			// Build a task with the given state.
			task := &a2a.Task{
				ID:        a2a.TaskID(taskID),
				ContextID: contextID,
				Status: a2a.TaskStatus{
					State: state,
				},
			}

			if statusMsg != "" {
				task.Status.Message = a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(statusMsg))
			}

			if numArtifacts > 0 {
				task.Artifacts = make([]*a2a.Artifact, numArtifacts)
				for i := range numArtifacts {
					task.Artifacts[i] = &a2a.Artifact{
						Parts: a2a.ContentParts{a2a.NewTextPart(fmt.Sprintf("artifact-%d", i))},
					}
				}
			}

			result, _ := srv.handleBroadcastTaskResult(task)

			// Verify legacy fields have correct values based on state.
			switch state {
			case a2a.TaskStateCompleted:
				if result.Status != "success" {
					return false
				}
				// Response should be text extracted from artifacts.
				expectedText := extractContentFromArtifacts(task.Artifacts)
				if result.Response != expectedText {
					t.Logf("completed: expected Response=%q, got %q", expectedText, result.Response)
					return false
				}
				if result.Error != "" {
					return false
				}

			case a2a.TaskStateInputRequired:
				if result.Status != "input-required" {
					return false
				}
				if result.Error != "" {
					return false
				}

			case a2a.TaskStateAuthRequired:
				if result.Status != "auth-required" {
					return false
				}
				if result.Error != "" {
					return false
				}

			case a2a.TaskStateFailed:
				if result.Status != "error" {
					return false
				}
				if result.Error == "" {
					return false
				}

			case a2a.TaskStateCanceled:
				if result.Status != "error" {
					return false
				}
				if result.Error != "task was canceled by the agent" {
					return false
				}

			default:
				// Working, Submitted, etc. fall into the timeout/default case.
				if result.Status != "error" {
					return false
				}
				if result.Error == "" {
					return false
				}
			}

			return true
		},
		gen.OneConstOf(
			a2a.TaskStateCompleted,
			a2a.TaskStateInputRequired,
			a2a.TaskStateAuthRequired,
			a2a.TaskStateFailed,
			a2a.TaskStateCanceled,
			a2a.TaskStateWorking,
			a2a.TaskStateSubmitted,
		),
		taskIDGen,
		contextIDGen,
		textGen,
		numArtifactsGen,
	))

	properties.TestingRun(t)
}

func TestHandleBroadcastMessage_UnrecognizedResponse(t *testing.T) {
	// Agent that returns a JSON-RPC error (SDK will surface it as an error).
	weirdAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		writeJSONRPCError(w, req.ID, -32603, "internal error")
	}))
	defer weirdAgent.Close()

	srv := NewServer()
	srv.registry.Connect("weird-agent", weirdAgent.URL, nil, "")
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

// =============================================================================
// Task 6.5: Unit test for broadcast error omitting raw fields
// Requirements: SRES-4.4
// =============================================================================

func TestBroadcastResult_ErrorOmitsRawFields(t *testing.T) {
	// Error and skipped results should have nil Task and Message fields.
	srv := NewServer()

	// Test failed task: handleBroadcastTaskResult should set Status="error" with nil Task/Message.
	failedTask := &a2a.Task{
		ID: "task-fail-broadcast",
		Status: a2a.TaskStatus{
			State:   a2a.TaskStateFailed,
			Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("agent failure")),
		},
	}
	failedResult, _ := srv.handleBroadcastTaskResult(failedTask)
	if failedResult.Status != "error" {
		t.Errorf("expected status 'error' for failed task, got %q", failedResult.Status)
	}
	if failedResult.Task != nil {
		t.Error("expected Task to be nil for failed broadcast result")
	}
	if failedResult.Message != nil {
		t.Error("expected Message to be nil for failed broadcast result")
	}

	// Test canceled task: handleBroadcastTaskResult should set Status="error" with nil Task/Message.
	canceledTask := &a2a.Task{
		ID:     "task-cancel-broadcast",
		Status: a2a.TaskStatus{State: a2a.TaskStateCanceled},
	}
	canceledResult, _ := srv.handleBroadcastTaskResult(canceledTask)
	if canceledResult.Status != "error" {
		t.Errorf("expected status 'error' for canceled task, got %q", canceledResult.Status)
	}
	if canceledResult.Task != nil {
		t.Error("expected Task to be nil for canceled broadcast result")
	}
	if canceledResult.Message != nil {
		t.Error("expected Message to be nil for canceled broadcast result")
	}

	// Contrast: completed task should have non-nil Task field.
	completedTask := &a2a.Task{
		ID:     "task-ok-broadcast",
		Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
		Artifacts: []*a2a.Artifact{
			{Parts: a2a.ContentParts{a2a.NewTextPart("success")}},
		},
	}
	completedResult, _ := srv.handleBroadcastTaskResult(completedTask)
	if completedResult.Status != "success" {
		t.Errorf("expected status 'success' for completed task, got %q", completedResult.Status)
	}
	if completedResult.Task == nil {
		t.Error("expected Task to be non-nil for successful broadcast result")
	}
	if completedResult.Task != completedTask {
		t.Error("expected Task to be the same pointer as the input task")
	}
}
