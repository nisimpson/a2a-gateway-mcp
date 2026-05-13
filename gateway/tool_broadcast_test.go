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

			// Set up httptest.Server for valid agents.
			agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				task := &a2a.Task{
					ID:     "task-broadcast",
					Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
					Artifacts: []*a2a.Artifact{
						{Parts: a2a.ContentParts{a2a.NewTextPart("broadcast response")}},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(task)
			}))
			defer agent.Close()

			// Create server and register valid aliases.
			srv := NewServer()
			for _, alias := range validAliases {
				srv.registry.Connect(alias, agent.URL, nil)
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
		task := &a2a.Task{
			ID:     "task-1",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("hello from agent")}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(task)
	}))
	defer agent.Close()

	srv := NewServer()
	srv.registry.Connect("agent-a", agent.URL, nil)
	srv.registry.Connect("agent-b", agent.URL, nil)
	srv.registry.Connect("agent-c", agent.URL, nil)

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
	// Use a server that immediately closes connections.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a failed task.
		task := &a2a.Task{
			ID: "task-fail",
			Status: a2a.TaskStatus{
				State:   a2a.TaskStateFailed,
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("internal error")),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(task)
	}))
	defer agent.Close()

	srv := NewServer()
	srv.registry.Connect("fail-a", agent.URL, nil)
	srv.registry.Connect("fail-b", agent.URL, nil)

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
		task := &a2a.Task{
			ID:     "task-good",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("success response")}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(task)
	}))
	defer goodAgent.Close()

	// Bad agent returns failed task.
	badAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		task := &a2a.Task{
			ID: "task-bad",
			Status: a2a.TaskStatus{
				State:   a2a.TaskStateFailed,
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("agent error")),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(task)
	}))
	defer badAgent.Close()

	srv := NewServer()
	srv.registry.Connect("good-agent", goodAgent.URL, nil)
	srv.registry.Connect("bad-agent", badAgent.URL, nil)

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
		task := &a2a.Task{
			ID:     "task-slow",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("should not see this")}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(task)
	}))
	defer agent.Close()

	srv := NewServer()
	srv.registry.Connect("slow-agent", agent.URL, nil)

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
		task := &a2a.Task{
			ID:     "task-concurrent",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("concurrent response")}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(task)
	}))
	defer agent.Close()

	srv := NewServer()
	numAgents := 5
	aliases := make([]string, numAgents)
	for i := 0; i < numAgents; i++ {
		alias := "concurrent-" + string(rune('a'+i))
		aliases[i] = alias
		srv.registry.Connect(alias, agent.URL, nil)
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
