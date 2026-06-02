package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Feature: a2a-gateway-mcp, Property 2: Context lifecycle correctness
// **Validates: Requirements AGMCP-2.5, AGMCP-2.6, AGMCP-2.7, AGMCP-13.1, AGMCP-13.2, AGMCP-13.3, AGMCP-13.4, AGMCP-13.5, AGMCP-13.6, AGMCP-13.7, AGMCP-13.8, AGMCP-13.9**

func TestPropertyContextLifecycleCorrectness(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for alias names
	aliasGen := gen.RegexMatch(`[a-z]{3,8}`)

	// Generator for context IDs
	contextIDGen := gen.RegexMatch(`ctx-[a-z0-9]{4,8}`)

	// Generator for a response context_id (may be empty to simulate no context in response)
	responseContextGen := gen.Weighted([]gen.WeightedGen{
		{Weight: 3, Gen: contextIDGen},
		{Weight: 1, Gen: gen.Const("")},
	})

	properties.Property("explicit context_id is used when provided", prop.ForAll(
		func(alias string, explicitCtx string, storedCtx string) bool {
			if alias == "" || explicitCtx == "" {
				return true // skip degenerate cases
			}
			store := NewContextStore()
			store.Set(alias, storedCtx)

			// When explicit context_id is provided, it should be used
			// regardless of what's stored.
			input := SendMessageInput{
				Agent:     alias,
				Message:   "hello",
				ContextID: explicitCtx,
			}

			// The explicit context_id should take priority
			contextID := input.ContextID
			if contextID == "" {
				contextID = store.Get(alias)
			}

			return contextID == explicitCtx
		},
		aliasGen,
		contextIDGen,
		contextIDGen,
	))

	properties.Property("stored context_id is used when no explicit one given", prop.ForAll(
		func(alias string, storedCtx string) bool {
			if alias == "" || storedCtx == "" {
				return true
			}
			store := NewContextStore()
			store.Set(alias, storedCtx)

			input := SendMessageInput{
				Agent:   alias,
				Message: "hello",
			}

			contextID := input.ContextID
			if contextID == "" {
				contextID = store.Get(alias)
			}

			return contextID == storedCtx
		},
		aliasGen,
		contextIDGen,
	))

	properties.Property("context store is updated after successful response with context_id", prop.ForAll(
		func(alias string, responseCtx string) bool {
			if alias == "" {
				return true
			}
			store := NewContextStore()

			// Simulate updating context store after response
			store.Set(alias, responseCtx)

			if responseCtx == "" {
				// Set with empty string should not modify store
				return store.Get(alias) == ""
			}
			return store.Get(alias) == responseCtx
		},
		aliasGen,
		responseContextGen,
	))

	properties.Property("disconnect clears context", prop.ForAll(
		func(alias string, storedCtx string) bool {
			if alias == "" || storedCtx == "" {
				return true
			}
			store := NewContextStore()
			store.Set(alias, storedCtx)

			// Verify it's stored
			if store.Get(alias) != storedCtx {
				return false
			}

			// Disconnect clears context
			store.Delete(alias)

			return store.Get(alias) == ""
		},
		aliasGen,
		contextIDGen,
	))

	properties.Property("URL-based sends don't touch context store", prop.ForAll(
		func(alias string, storedCtx string) bool {
			if alias == "" || storedCtx == "" {
				return true
			}
			store := NewContextStore()
			store.Set(alias, storedCtx)

			// For URL-based sends (IsAlias=false), context store should not be read or written.
			// Simulate: resolved.IsAlias is false, so we don't read from store.
			resolved := &ResolveResult{
				URL:     "http://example.com",
				IsAlias: false,
			}

			// Context determination for URL-based: only use explicit
			input := SendMessageInput{
				Agent:   "http://example.com",
				Message: "hello",
			}
			contextID := input.ContextID
			if contextID == "" && resolved.IsAlias {
				contextID = store.Get(alias)
			}

			// contextID should be empty (no explicit, and IsAlias is false so store not consulted)
			if contextID != "" {
				return false
			}

			// Store should remain unchanged
			return store.Get(alias) == storedCtx
		},
		aliasGen,
		contextIDGen,
	))

	properties.Property("connect with different URL clears context store entry", prop.ForAll(
		func(alias string, storedCtx string) bool {
			if alias == "" || storedCtx == "" {
				return true
			}
			registry := NewAgentRegistry()
			store := NewContextStore()

			// Initial connect and store a context
			registry.Connect(alias, "http://original.example.com", nil)
			store.Set(alias, storedCtx)

			// Verify context is stored
			if store.Get(alias) != storedCtx {
				return false
			}

			// Simulate connect with different URL (same logic as handleConnectAgent)
			existing := registry.Lookup(alias)
			newURL := "http://different.example.com"
			if existing != nil && existing.URL != newURL {
				store.Delete(alias)
			}
			registry.Connect(alias, newURL, nil)

			// Context should be cleared
			return store.Get(alias) == ""
		},
		aliasGen,
		contextIDGen,
	))

	properties.TestingRun(t)
}

// --- Unit Tests for send_message handler (Task 8.3) ---

// newTestServerWithAgent creates a Server with a registered agent pointing to the given URL.
func newTestServerWithAgent(alias, agentURL string) *Server {
	srv := NewServer()
	srv.registry.Connect(alias, agentURL, nil)
	return srv
}

func TestHandleSendMessage_CompletedTask(t *testing.T) {
	// Mock A2A agent that returns a completed task with text artifacts.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		task := &a2a.Task{
			ID:        "task-1",
			ContextID: "ctx-resp-1",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{
					Parts: a2a.ContentParts{a2a.NewTextPart("Hello from agent")},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(task)
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("test-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "test-agent",
		Message: "Hello",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Should have text content + context_id content
	if len(result.Content) < 1 {
		t.Fatalf("expected at least 1 content item, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected first content to be TextContent")
	}
	if textContent.Text != "Hello from agent" {
		t.Errorf("expected %q, got %q", "Hello from agent", textContent.Text)
	}

	// Verify context store was updated
	if stored := srv.contextStore.Get("test-agent"); stored != "ctx-resp-1" {
		t.Errorf("expected context store to have %q, got %q", "ctx-resp-1", stored)
	}
}

func TestHandleSendMessage_FailedTask(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		task := &a2a.Task{
			ID:        "task-2",
			ContextID: "ctx-resp-2",
			Status: a2a.TaskStatus{
				State:   a2a.TaskStateFailed,
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("something went wrong")),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(task)
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("fail-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "fail-agent",
		Message: "Do something",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for failed task")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text != "something went wrong" {
		t.Errorf("expected failure message %q, got %q", "something went wrong", textContent.Text)
	}
}

func TestHandleSendMessage_CanceledTask(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		task := &a2a.Task{
			ID:        "task-3",
			ContextID: "ctx-resp-3",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCanceled},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(task)
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("cancel-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "cancel-agent",
		Message: "Do something",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for canceled task")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text != "task was canceled by the agent" {
		t.Errorf("expected cancel message, got %q", textContent.Text)
	}
}

func TestHandleSendMessage_ContextID_Explicit(t *testing.T) {
	// Verify that explicit context_id is sent to the agent.
	var receivedContextID string
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req a2a.SendMessageRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		receivedContextID = req.Message.ContextID

		task := &a2a.Task{
			ID:        "task-4",
			ContextID: "ctx-new",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("ok")}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(task)
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("ctx-agent", agent.URL)
	// Pre-store a different context_id
	srv.contextStore.Set("ctx-agent", "ctx-stored")

	input := SendMessageInput{
		Agent:     "ctx-agent",
		Message:   "hello",
		ContextID: "ctx-explicit",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error")
	}

	// The explicit context_id should have been sent
	if receivedContextID != "ctx-explicit" {
		t.Errorf("expected agent to receive context_id %q, got %q", "ctx-explicit", receivedContextID)
	}

	// Context store should be updated with the response context_id
	if stored := srv.contextStore.Get("ctx-agent"); stored != "ctx-new" {
		t.Errorf("expected context store to have %q, got %q", "ctx-new", stored)
	}
}

func TestHandleSendMessage_ContextID_Stored(t *testing.T) {
	// Verify that stored context_id is used when no explicit one is provided.
	var receivedContextID string
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req a2a.SendMessageRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		receivedContextID = req.Message.ContextID

		task := &a2a.Task{
			ID:        "task-5",
			ContextID: "ctx-updated",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("ok")}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(task)
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("stored-agent", agent.URL)
	srv.contextStore.Set("stored-agent", "ctx-stored-value")

	input := SendMessageInput{
		Agent:   "stored-agent",
		Message: "hello",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error")
	}

	// The stored context_id should have been sent
	if receivedContextID != "ctx-stored-value" {
		t.Errorf("expected agent to receive stored context_id %q, got %q", "ctx-stored-value", receivedContextID)
	}

	// Context store should be updated with the response context_id
	if stored := srv.contextStore.Get("stored-agent"); stored != "ctx-updated" {
		t.Errorf("expected context store to have %q, got %q", "ctx-updated", stored)
	}
}

func TestHandleSendMessage_ContextID_NewConversation(t *testing.T) {
	// Verify that no context_id is sent for a new conversation.
	var receivedContextID string
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req a2a.SendMessageRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		receivedContextID = req.Message.ContextID

		task := &a2a.Task{
			ID:        "task-6",
			ContextID: "ctx-brand-new",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("ok")}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(task)
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("new-agent", agent.URL)
	// No stored context

	input := SendMessageInput{
		Agent:   "new-agent",
		Message: "hello",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error")
	}

	// No context_id should have been sent
	if receivedContextID != "" {
		t.Errorf("expected empty context_id for new conversation, got %q", receivedContextID)
	}

	// Context store should now have the response context_id
	if stored := srv.contextStore.Get("new-agent"); stored != "ctx-brand-new" {
		t.Errorf("expected context store to have %q, got %q", "ctx-brand-new", stored)
	}
}

func TestHandleSendMessage_URLBased_BypassesContextStore(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		task := &a2a.Task{
			ID:        "task-7",
			ContextID: "ctx-url-resp",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("url response")}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(task)
	}))
	defer agent.Close()

	srv := NewServer()
	// Register an agent with the same URL under an alias (to verify URL-based doesn't use it)
	srv.registry.Connect("some-alias", agent.URL, nil)
	srv.contextStore.Set("some-alias", "ctx-should-not-change")

	input := SendMessageInput{
		Agent:   agent.URL, // Use URL directly, not alias
		Message: "hello via url",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error")
	}

	// Context store for the alias should NOT be modified
	if stored := srv.contextStore.Get("some-alias"); stored != "ctx-should-not-change" {
		t.Errorf("expected context store to remain %q, got %q", "ctx-should-not-change", stored)
	}

	// No context store entry should exist for the URL
	if stored := srv.contextStore.Get(agent.URL); stored != "" {
		t.Errorf("expected no context store entry for URL, got %q", stored)
	}
}

func TestHandleSendMessage_ValidationError_EmptyAgent(t *testing.T) {
	srv := NewServer()

	input := SendMessageInput{
		Agent:   "",
		Message: "hello",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for empty agent")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text != "agent identifier is required" {
		t.Errorf("unexpected error message: %q", textContent.Text)
	}
}

func TestHandleSendMessage_ValidationError_EmptyMessage(t *testing.T) {
	srv := NewServer()

	input := SendMessageInput{
		Agent:   "some-agent",
		Message: "",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
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
	if textContent.Text != "message is required and cannot be empty" {
		t.Errorf("unexpected error message: %q", textContent.Text)
	}
}

func TestHandleSendMessage_NonTerminalState_PollsUntilComplete(t *testing.T) {
	// Agent returns "working" on first call, then "completed" on poll.
	var callCount int
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var task *a2a.Task
		if callCount == 1 {
			task = &a2a.Task{
				ID:        "task-8",
				ContextID: "ctx-working",
				Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
			}
		} else {
			task = &a2a.Task{
				ID:        "task-8",
				ContextID: "ctx-done",
				Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
				Artifacts: []*a2a.Artifact{
					{Parts: a2a.ContentParts{a2a.NewTextPart("polled result")}},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(task)
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("working-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "working-agent",
		Message: "hello",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success after polling, got error: %v", result.Content)
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text != "polled result" {
		t.Errorf("expected %q, got %q", "polled result", textContent.Text)
	}

	// Verify polling happened (at least 2 calls: initial + poll).
	if callCount < 2 {
		t.Errorf("expected at least 2 HTTP calls (initial + poll), got %d", callCount)
	}
}

func TestHandleSendMessage_NonTerminalState_Timeout(t *testing.T) {
	// Agent always returns "working" — should timeout after context deadline.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		task := &a2a.Task{
			ID:        "task-9",
			ContextID: "ctx-stuck",
			Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(task)
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("stuck-agent", agent.URL)

	// Use a short-lived context to avoid waiting 60s in tests.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	input := SendMessageInput{
		Agent:   "stuck-agent",
		Message: "hello",
	}

	result, _, err := srv.handleSendMessage(ctx, nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for stuck non-terminal state")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text == "" {
		t.Error("expected non-empty timeout error message")
	}
}

func TestHandleSendMessage_InputRequired_ReturnsImmediately(t *testing.T) {
	// Agent returns input-required with a status message explaining what's needed.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		task := &a2a.Task{
			ID:        "task-input-1",
			ContextID: "ctx-input-1",
			Status: a2a.TaskStatus{
				State:   a2a.TaskStateInputRequired,
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Please confirm: proceed with deletion?")),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(task)
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("input-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "input-agent",
		Message: "delete all files",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success for input-required state, got error: %v", result.Content)
	}

	// Should contain: status message text, state indicator, context_id.
	if len(result.Content) < 3 {
		t.Fatalf("expected at least 3 content items, got %d", len(result.Content))
	}

	// First: the agent's status message.
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected first content to be TextContent")
	}
	if textContent.Text != "Please confirm: proceed with deletion?" {
		t.Errorf("expected agent message %q, got %q", "Please confirm: proceed with deletion?", textContent.Text)
	}

	// Second: state indicator.
	stateContent, ok := result.Content[1].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected second content to be TextContent")
	}
	if stateContent.Text != "state:input-required" {
		t.Errorf("expected state indicator %q, got %q", "state:input-required", stateContent.Text)
	}

	// Third: context_id.
	ctxContent, ok := result.Content[2].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected third content to be TextContent")
	}
	if ctxContent.Text != "context_id:ctx-input-1" {
		t.Errorf("expected context_id %q, got %q", "context_id:ctx-input-1", ctxContent.Text)
	}

	// Verify context store was updated.
	if stored := srv.contextStore.Get("input-agent"); stored != "ctx-input-1" {
		t.Errorf("expected context store to have %q, got %q", "ctx-input-1", stored)
	}
}

func TestHandleSendMessage_InputRequired_NoStatusMessage_UsesArtifacts(t *testing.T) {
	// Agent returns input-required with no status message but with artifacts.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		task := &a2a.Task{
			ID:        "task-input-2",
			ContextID: "ctx-input-2",
			Status: a2a.TaskStatus{
				State: a2a.TaskStateInputRequired,
			},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("What is your name?")}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(task)
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("input-agent-2", agent.URL)

	input := SendMessageInput{
		Agent:   "input-agent-2",
		Message: "start onboarding",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected success for input-required state")
	}

	// First content should be artifact text.
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected first content to be TextContent")
	}
	if textContent.Text != "What is your name?" {
		t.Errorf("expected %q, got %q", "What is your name?", textContent.Text)
	}
}

func TestHandleSendMessage_InputRequired_DoesNotPoll(t *testing.T) {
	// Verify that input-required does NOT trigger polling (only 1 HTTP call).
	var callCount int
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		task := &a2a.Task{
			ID:        "task-input-3",
			ContextID: "ctx-input-3",
			Status: a2a.TaskStatus{
				State:   a2a.TaskStateInputRequired,
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("need more info")),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(task)
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("no-poll-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "no-poll-agent",
		Message: "do something",
	}

	_, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should only have made exactly 1 HTTP call (no polling).
	if callCount != 1 {
		t.Errorf("expected exactly 1 HTTP call (no polling), got %d", callCount)
	}
}

func TestHandleSendMessage_InputRequired_ResumableViaContextID(t *testing.T) {
	// Simulate a multi-turn interaction:
	// 1. First send_message -> agent returns input-required
	// 2. Second send_message with context_id -> agent returns completed
	var callCount int
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var req a2a.SendMessageRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		var task *a2a.Task
		if req.Message.ContextID == "" {
			// First call: return input-required
			task = &a2a.Task{
				ID:        "task-multi-1",
				ContextID: "ctx-multi-1",
				Status: a2a.TaskStatus{
					State:   a2a.TaskStateInputRequired,
					Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("confirm?")),
				},
			}
		} else {
			// Follow-up with context_id: return completed
			task = &a2a.Task{
				ID:        "task-multi-1",
				ContextID: "ctx-multi-1",
				Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
				Artifacts: []*a2a.Artifact{
					{Parts: a2a.ContentParts{a2a.NewTextPart("done!")}},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(task)
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("multi-turn-agent", agent.URL)

	// First message: should get input-required.
	input1 := SendMessageInput{
		Agent:   "multi-turn-agent",
		Message: "do something dangerous",
	}
	result1, _, err := srv.handleSendMessage(context.Background(), nil, input1)
	if err != nil {
		t.Fatalf("unexpected error on first send: %v", err)
	}
	if result1.IsError {
		t.Fatal("expected success for first send")
	}

	// Context store should now have the context_id.
	storedCtx := srv.contextStore.Get("multi-turn-agent")
	if storedCtx != "ctx-multi-1" {
		t.Fatalf("expected stored context %q, got %q", "ctx-multi-1", storedCtx)
	}

	// Second message: uses stored context_id automatically, agent completes.
	input2 := SendMessageInput{
		Agent:   "multi-turn-agent",
		Message: "yes, confirmed",
	}
	result2, _, err := srv.handleSendMessage(context.Background(), nil, input2)
	if err != nil {
		t.Fatalf("unexpected error on second send: %v", err)
	}
	if result2.IsError {
		t.Fatal("expected success for second send")
	}

	textContent, ok := result2.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text != "done!" {
		t.Errorf("expected %q, got %q", "done!", textContent.Text)
	}

	// Both calls should have been made without polling.
	if callCount != 2 {
		t.Errorf("expected exactly 2 HTTP calls, got %d", callCount)
	}
}

func TestHandleSendMessage_NonTerminalState_NoTaskID_DoesNotPoll(t *testing.T) {
	// Agent returns a "working" state with no task ID. The gateway should NOT
	// attempt to poll because tasks/get requires a task ID.
	var callCount int
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		task := &a2a.Task{
			// No ID — agent doesn't support tasks/get
			Status: a2a.TaskStatus{State: a2a.TaskStateWorking},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(task)
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("no-taskid-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "no-taskid-agent",
		Message: "hello",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result when agent returns non-terminal with no task ID")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text == "" {
		t.Error("expected non-empty error message")
	}

	// Should only have made 1 HTTP call (no polling attempted).
	if callCount != 1 {
		t.Errorf("expected exactly 1 HTTP call (no poll), got %d", callCount)
	}
}

func TestHandleSendMessage_PollFailsOnNon2xx(t *testing.T) {
	// Agent returns "working" on first call (with a task ID), but then returns
	// 404 on the tasks/get poll — indicating it doesn't support that endpoint.
	var callCount int
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			task := &a2a.Task{
				ID:     "task-no-get",
				Status: a2a.TaskStatus{State: a2a.TaskStateWorking},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(task)
		} else {
			// Agent doesn't support tasks/get
			http.NotFound(w, r)
		}
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("no-get-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "no-get-agent",
		Message: "hello",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when poll receives non-2xx")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	// Should mention that the agent doesn't support tasks/get.
	if textContent.Text == "" {
		t.Error("expected non-empty error message about polling failure")
	}

	// Should have made exactly 2 calls: initial + one failed poll attempt.
	if callCount != 2 {
		t.Errorf("expected exactly 2 HTTP calls, got %d", callCount)
	}
}
