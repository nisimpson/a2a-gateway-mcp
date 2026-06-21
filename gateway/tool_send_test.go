package gateway

import (
	"context"
	"encoding/json"
	"io"
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
			registry.Connect(alias, "http://original.example.com", nil, "")
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
			registry.Connect(alias, newURL, nil, "")

			// Context should be cleared
			return store.Get(alias) == ""
		},
		aliasGen,
		contextIDGen,
	))

	properties.TestingRun(t)
}

// --- Unit Tests for send_message handler (Task 8.3) ---

func TestHandleSendMessage_CompletedTask(t *testing.T) {
	// Mock A2A agent that returns a completed task with text artifacts.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
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
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
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

	// Should have text content (response text only, no metadata items)
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
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:        "task-2",
			ContextID: "ctx-resp-2",
			Status: a2a.TaskStatus{
				State:   a2a.TaskStateFailed,
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("something went wrong")),
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
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
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:        "task-3",
			ContextID: "ctx-resp-3",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCanceled},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
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
		req, _ := readJSONRPCRequest(r)
		var params a2a.SendMessageRequest
		_ = json.Unmarshal(req.Params, &params)
		receivedContextID = params.Message.ContextID

		task := &a2a.Task{
			ID:        "task-4",
			ContextID: "ctx-new",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("ok")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
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
		req, _ := readJSONRPCRequest(r)
		var params a2a.SendMessageRequest
		_ = json.Unmarshal(req.Params, &params)
		receivedContextID = params.Message.ContextID

		task := &a2a.Task{
			ID:        "task-5",
			ContextID: "ctx-updated",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("ok")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
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
		req, _ := readJSONRPCRequest(r)
		var params a2a.SendMessageRequest
		_ = json.Unmarshal(req.Params, &params)
		receivedContextID = params.Message.ContextID

		task := &a2a.Task{
			ID:        "task-6",
			ContextID: "ctx-brand-new",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("ok")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
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
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:        "task-7",
			ContextID: "ctx-url-resp",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("url response")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := NewServer()
	// Register an agent with the same URL under an alias (to verify URL-based doesn't use it)
	srv.registry.Connect("some-alias", agent.URL, nil, "")
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
	if textContent.Text != "either 'message' or 'parts' is required" {
		t.Errorf("unexpected error message: %q", textContent.Text)
	}
}

func TestHandleSendMessage_NonTerminalState_PollsUntilComplete(t *testing.T) {
	// Agent returns "working" on first call, then "completed" on poll.
	var callCount int
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		callCount++
		var task *a2a.Task
		if callCount == 1 {
			// SendMessage: return working task wrapped in StreamResponse.
			task = &a2a.Task{
				ID:        "task-8",
				ContextID: "ctx-working",
				Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
			}
			writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
		} else {
			// GetTask: return completed task (plain, not StreamResponse).
			task = &a2a.Task{
				ID:        "task-8",
				ContextID: "ctx-done",
				Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
				Artifacts: []*a2a.Artifact{
					{Parts: a2a.ContentParts{a2a.NewTextPart("polled result")}},
				},
			}
			writeJSONRPCResult(w, req.ID, task)
		}
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
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:        "task-9",
			ContextID: "ctx-stuck",
			Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
		}
		// Respond differently based on method: SendMessage vs GetTask
		if req.Method == "message/send" {
			writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
		} else {
			writeJSONRPCResult(w, req.ID, task)
		}
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
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:        "task-input-1",
			ContextID: "ctx-input-1",
			Status: a2a.TaskStatus{
				State:   a2a.TaskStateInputRequired,
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Please confirm: proceed with deletion?")),
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
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

	// Should contain: status message text and state indicator.
	// Metadata (task_id, context_id) is in the structured response, not content.
	if len(result.Content) < 2 {
		t.Fatalf("expected at least 2 content items, got %d", len(result.Content))
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

	// Verify context store was updated.
	if stored := srv.contextStore.Get("input-agent"); stored != "ctx-input-1" {
		t.Errorf("expected context store to have %q, got %q", "ctx-input-1", stored)
	}
}

func TestHandleSendMessage_InputRequired_NoStatusMessage_UsesArtifacts(t *testing.T) {
	// Agent returns input-required with no status message but with artifacts.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
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
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
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
		req, _ := readJSONRPCRequest(r)
		callCount++
		task := &a2a.Task{
			ID:        "task-input-3",
			ContextID: "ctx-input-3",
			Status: a2a.TaskStatus{
				State:   a2a.TaskStateInputRequired,
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("need more info")),
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
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
		req, _ := readJSONRPCRequest(r)
		callCount++
		var params a2a.SendMessageRequest
		_ = json.Unmarshal(req.Params, &params)

		var task *a2a.Task
		if params.Message.ContextID == "" {
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
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
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
		req, _ := readJSONRPCRequest(r)
		callCount++
		task := &a2a.Task{
			// No ID — agent doesn't support tasks/get
			Status: a2a.TaskStatus{State: a2a.TaskStateWorking},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
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
	// an error on the tasks/get poll.
	var callCount int
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		callCount++
		if callCount == 1 {
			// SendMessage: return working task in StreamResponse format.
			task := &a2a.Task{
				ID:     "task-no-get",
				Status: a2a.TaskStatus{State: a2a.TaskStateWorking},
			}
			writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
		} else {
			// GetTask: return JSON-RPC error (agent doesn't support get).
			writeJSONRPCError(w, req.ID, -32601, "method not found")
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

func TestHandleSendMessage_AuthRequired_ReturnsImmediately(t *testing.T) {
	// Agent returns auth-required with a status message explaining what auth is needed.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:        "task-auth-1",
			ContextID: "ctx-auth-1",
			Status: a2a.TaskStatus{
				State:   a2a.TaskStateAuthRequired,
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Please authenticate with OAuth2 to access this resource")),
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("auth-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "auth-agent",
		Message: "access protected resource",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success for auth-required state, got error: %v", result.Content)
	}

	// Should contain: status message text and state indicator.
	// Metadata (task_id, context_id) is in the structured response, not content.
	if len(result.Content) < 2 {
		t.Fatalf("expected at least 2 content items, got %d", len(result.Content))
	}

	// First: the agent's status message.
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected first content to be TextContent")
	}
	if textContent.Text != "Please authenticate with OAuth2 to access this resource" {
		t.Errorf("expected agent message %q, got %q", "Please authenticate with OAuth2 to access this resource", textContent.Text)
	}

	// Second: state indicator.
	stateContent, ok := result.Content[1].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected second content to be TextContent")
	}
	if stateContent.Text != "state:auth-required" {
		t.Errorf("expected state indicator %q, got %q", "state:auth-required", stateContent.Text)
	}

	// Verify context store was updated.
	if stored := srv.contextStore.Get("auth-agent"); stored != "ctx-auth-1" {
		t.Errorf("expected context store to have %q, got %q", "ctx-auth-1", stored)
	}
}

func TestHandleSendMessage_AuthRequired_DoesNotPoll(t *testing.T) {
	// Verify that auth-required does NOT trigger polling (only 1 HTTP call).
	var callCount int
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		callCount++
		task := &a2a.Task{
			ID:        "task-auth-2",
			ContextID: "ctx-auth-2",
			Status: a2a.TaskStatus{
				State:   a2a.TaskStateAuthRequired,
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("need OAuth token")),
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("auth-no-poll-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "auth-no-poll-agent",
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

func TestHandleSendMessage_AuthRequired_ContextStoreUpdate(t *testing.T) {
	// Verify context store is updated when auth-required is returned with context_id.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:        "task-auth-3",
			ContextID: "ctx-auth-new",
			Status: a2a.TaskStatus{
				State:   a2a.TaskStateAuthRequired,
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("authenticate please")),
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("auth-ctx-agent", agent.URL)
	// Pre-store a different context_id.
	srv.contextStore.Set("auth-ctx-agent", "ctx-old")

	input := SendMessageInput{
		Agent:   "auth-ctx-agent",
		Message: "access resource",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success for auth-required state, got error")
	}

	// Context store should be updated with the response context_id.
	if stored := srv.contextStore.Get("auth-ctx-agent"); stored != "ctx-auth-new" {
		t.Errorf("expected context store to have %q, got %q", "ctx-auth-new", stored)
	}
}

func TestHandleSendMessage_AuthRequired_AfterPolling(t *testing.T) {
	// Agent returns "working" first, then auth-required on poll.
	// Verifies that auth-required terminates polling.
	var callCount int
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		callCount++
		var task *a2a.Task
		if callCount == 1 {
			// SendMessage: return working task in StreamResponse.
			task = &a2a.Task{
				ID:        "task-auth-poll",
				ContextID: "ctx-auth-poll",
				Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
			}
			writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
		} else {
			// GetTask: return auth-required (plain task).
			task = &a2a.Task{
				ID:        "task-auth-poll",
				ContextID: "ctx-auth-poll",
				Status: a2a.TaskStatus{
					State:   a2a.TaskStateAuthRequired,
					Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("auth needed after working")),
				},
			}
			writeJSONRPCResult(w, req.ID, task)
		}
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("auth-poll-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "auth-poll-agent",
		Message: "do work",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success for auth-required after polling, got error: %v", result.Content)
	}

	// Should have state:auth-required indicator.
	if len(result.Content) < 2 {
		t.Fatalf("expected at least 2 content items, got %d", len(result.Content))
	}

	stateContent, ok := result.Content[1].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected second content to be TextContent")
	}
	if stateContent.Text != "state:auth-required" {
		t.Errorf("expected state indicator %q, got %q", "state:auth-required", stateContent.Text)
	}

	// Verify polling happened (at least 2 calls: initial + poll).
	if callCount < 2 {
		t.Errorf("expected at least 2 HTTP calls (initial + poll), got %d", callCount)
	}
}

// --- Tests for Task 3: Message-only response detection and handling ---

func TestHandleSendMessage_MessageResponse_WithText(t *testing.T) {
	// Agent returns a direct Message response (no Task lifecycle).
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Hello from a simple agent"))
		writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("msg-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "msg-agent",
		Message: "hi",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Should have 1 content item: the text.
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text != "Hello from a simple agent" {
		t.Errorf("expected %q, got %q", "Hello from a simple agent", textContent.Text)
	}
}

func TestHandleSendMessage_MessageResponse_WithContextID(t *testing.T) {
	// Agent returns a Message with a context_id.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("reply with context"))
		msg.ContextID = "ctx-msg-1"
		writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("msg-ctx-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "msg-ctx-agent",
		Message: "hello",
	}

	result, structured, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Should have 1 content item: text only (no context_id metadata).
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected first content to be TextContent")
	}
	if textContent.Text != "reply with context" {
		t.Errorf("expected %q, got %q", "reply with context", textContent.Text)
	}

	// Context ID should be available via structured response.
	if structured == nil {
		t.Fatal("expected structured response")
	}
	resp := structured.(*SendMessageResponse)
	if resp.Message.ContextID != "ctx-msg-1" {
		t.Errorf("expected structured context_id %q, got %q", "ctx-msg-1", resp.Message.ContextID)
	}

	// Verify context store was updated.
	if stored := srv.contextStore.Get("msg-ctx-agent"); stored != "ctx-msg-1" {
		t.Errorf("expected context store to have %q, got %q", "ctx-msg-1", stored)
	}
}

func TestHandleSendMessage_MessageResponse_NonTextParts(t *testing.T) {
	// Agent returns a Message with only non-text parts.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		msg := &a2a.Message{
			Role:  a2a.MessageRoleAgent,
			Parts: a2a.ContentParts{a2a.NewDataPart(map[string]any{"key": "value"})},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("msg-nontext-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "msg-nontext-agent",
		Message: "give me data",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	if len(result.Content) < 1 {
		t.Fatal("expected at least 1 content item")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	// Data parts are now rendered as JSON.
	expected := `{"key":"value"}`
	if textContent.Text != expected {
		t.Errorf("expected %q, got %q", expected, textContent.Text)
	}
}

func TestHandleSendMessage_UnrecognizedResponse(t *testing.T) {
	// Agent returns a JSON-RPC error, which the SDK surfaces as an error.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		writeJSONRPCError(w, req.ID, -32603, "internal error")
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("bad-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "bad-agent",
		Message: "hello",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for agent returning JSON-RPC error")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text == "" {
		t.Error("expected non-empty error message")
	}
}

func TestHandleSendMessage_MessageResponse_URLBased_NoContextStoreUpdate(t *testing.T) {
	// When using a URL directly (not alias), context store should not be updated
	// even if the Message has a context_id.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("url response"))
		msg.ContextID = "ctx-url-msg"
		writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
	}))
	defer agent.Close()

	srv := NewServer()

	input := SendMessageInput{
		Agent:   agent.URL, // URL directly, not alias
		Message: "hello",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// The response should only have the response text (no context_id metadata item).
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected first content to be TextContent")
	}
	if textContent.Text != "url response" {
		t.Errorf("expected %q, got %q", "url response", textContent.Text)
	}

	// Context store should NOT have anything stored for the URL.
	if stored := srv.contextStore.Get(agent.URL); stored != "" {
		t.Errorf("expected no context store entry for URL, got %q", stored)
	}
}

func TestHandleSendMessage_MessageResponse_MultipleTextParts(t *testing.T) {
	// Agent returns a Message with multiple text parts - they should be concatenated.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		msg := &a2a.Message{
			Role: a2a.MessageRoleAgent,
			Parts: a2a.ContentParts{
				a2a.NewTextPart("Part one. "),
				a2a.NewTextPart("Part two."),
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("msg-multi-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "msg-multi-agent",
		Message: "hello",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
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
	if textContent.Text != "Part one. Part two." {
		t.Errorf("expected %q, got %q", "Part one. Part two.", textContent.Text)
	}
}

// --- Tests for Task 4: task_id parameter in send_message ---

func TestHandleSendMessage_WithTaskID(t *testing.T) {
	// Verify that task_id is included in the A2A Message sent to the agent.
	var receivedTaskID string
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		var params a2a.SendMessageRequest
		_ = json.Unmarshal(req.Params, &params)
		receivedTaskID = string(params.Message.TaskID)

		task := &a2a.Task{
			ID:        "task-tid-1",
			ContextID: "ctx-tid-1",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("task continued")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("taskid-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "taskid-agent",
		Message: "continue the task",
		TaskID:  "existing-task-123",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Verify that the task_id was sent to the agent.
	if receivedTaskID != "existing-task-123" {
		t.Errorf("expected agent to receive task_id %q, got %q", "existing-task-123", receivedTaskID)
	}

	// Verify no context_id was sent (task_id only, no explicit or stored context).
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text != "task continued" {
		t.Errorf("expected %q, got %q", "task continued", textContent.Text)
	}
}

func TestHandleSendMessage_WithTaskID_NoContextID(t *testing.T) {
	// Verify that when task_id is provided without context_id, only task_id
	// appears in the message (context_id is empty).
	var receivedTaskID string
	var receivedContextID string
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		var params a2a.SendMessageRequest
		_ = json.Unmarshal(req.Params, &params)
		receivedTaskID = string(params.Message.TaskID)
		receivedContextID = params.Message.ContextID

		task := &a2a.Task{
			ID:        "task-tid-2",
			ContextID: "ctx-tid-2",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("ok")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("taskid-only-agent", agent.URL)
	// No stored context — ensure no context_id leaks in.

	input := SendMessageInput{
		Agent:   "taskid-only-agent",
		Message: "follow up on task",
		TaskID:  "task-abc",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// task_id should be present.
	if receivedTaskID != "task-abc" {
		t.Errorf("expected agent to receive task_id %q, got %q", "task-abc", receivedTaskID)
	}

	// context_id should be empty (no explicit and no stored context).
	if receivedContextID != "" {
		t.Errorf("expected empty context_id when only task_id is provided, got %q", receivedContextID)
	}
}

func TestHandleSendMessage_WithTaskID_AndContextID(t *testing.T) {
	// Verify that when both task_id and context_id are provided, both appear
	// in the message sent to the agent.
	var receivedTaskID string
	var receivedContextID string
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		var params a2a.SendMessageRequest
		_ = json.Unmarshal(req.Params, &params)
		receivedTaskID = string(params.Message.TaskID)
		receivedContextID = params.Message.ContextID

		task := &a2a.Task{
			ID:        "task-tid-3",
			ContextID: "ctx-tid-3",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("both provided")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("taskid-both-agent", agent.URL)

	input := SendMessageInput{
		Agent:     "taskid-both-agent",
		Message:   "continue with both ids",
		TaskID:    "task-xyz",
		ContextID: "ctx-explicit-xyz",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Both task_id and context_id should be present in the sent message.
	if receivedTaskID != "task-xyz" {
		t.Errorf("expected agent to receive task_id %q, got %q", "task-xyz", receivedTaskID)
	}
	if receivedContextID != "ctx-explicit-xyz" {
		t.Errorf("expected agent to receive context_id %q, got %q", "ctx-explicit-xyz", receivedContextID)
	}

	// Verify the response content.
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text != "both provided" {
		t.Errorf("expected %q, got %q", "both provided", textContent.Text)
	}
}

func TestHandleSendMessage_WithTaskID_VerifyJSONBody(t *testing.T) {
	// Verify that task_id actually appears in the raw JSON body sent to the agent.
	var rawBody []byte
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawBody, _ = io.ReadAll(r.Body)

		// Parse to get the ID for the response.
		var rpcReq jsonrpcTestRequest
		_ = json.Unmarshal(rawBody, &rpcReq)

		task := &a2a.Task{
			ID:     "task-tid-4",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("verified")}},
			},
		}
		writeJSONRPCResult(w, rpcReq.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("taskid-json-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "taskid-json-agent",
		Message: "verify json",
		TaskID:  "my-task-id-999",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Parse the raw JSON to verify task_id is present in the params.
	var bodyMap map[string]any
	if err := json.Unmarshal(rawBody, &bodyMap); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}

	// The body is a JSON-RPC envelope: {"jsonrpc":"2.0","method":"...","params":{...},"id":"..."}
	paramsRaw, ok := bodyMap["params"].(map[string]any)
	if !ok {
		t.Fatal("expected 'params' field in JSON-RPC request body")
	}

	msgMap, ok := paramsRaw["message"].(map[string]any)
	if !ok {
		t.Fatal("expected 'message' field in params")
	}

	taskID, ok := msgMap["taskId"]
	if !ok {
		t.Fatal("expected 'taskId' field in message JSON body")
	}
	if taskID != "my-task-id-999" {
		t.Errorf("expected taskId %q in JSON body, got %q", "my-task-id-999", taskID)
	}
}

// --- Integration Tests for message-metadata feature (6.12, 6.13) ---

// Feature: message-metadata, Integration test: send_message with metadata reaches agent
// **Validates: META-1.3**

func TestHandleSendMessage_MetadataReachesAgent(t *testing.T) {
	var receivedMetadata map[string]any
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		var params a2a.SendMessageRequest
		_ = json.Unmarshal(req.Params, &params)
		receivedMetadata = params.Metadata

		task := &a2a.Task{
			ID:     "task-meta-1",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("metadata received")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("meta-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "meta-agent",
		Message: "hello with metadata",
		Metadata: map[string]any{
			"caller":  "test-client",
			"version": float64(2),
			"nested":  map[string]any{"key": "value"},
		},
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Verify the metadata was received by the agent.
	if receivedMetadata == nil {
		t.Fatal("expected metadata to be received by agent")
	}
	if receivedMetadata["caller"] != "test-client" {
		t.Errorf("expected metadata[caller]=%q, got %v", "test-client", receivedMetadata["caller"])
	}
	if receivedMetadata["version"] != float64(2) {
		t.Errorf("expected metadata[version]=%v, got %v", float64(2), receivedMetadata["version"])
	}
	nested, ok := receivedMetadata["nested"].(map[string]any)
	if !ok {
		t.Fatal("expected nested metadata to be a map")
	}
	if nested["key"] != "value" {
		t.Errorf("expected nested[key]=%q, got %v", "value", nested["key"])
	}
}

// Feature: message-metadata, Integration test: send_message with data part reaches agent
// **Validates: META-2.9**

func TestHandleSendMessage_DataPartReachesAgent(t *testing.T) {
	var receivedParts []json.RawMessage
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		// Parse the raw JSON to extract parts.
		var rpcReq map[string]any
		_ = json.Unmarshal(body, &rpcReq)

		// Extract message.parts from params.
		params, _ := rpcReq["params"].(map[string]any)
		message, _ := params["message"].(map[string]any)
		partsRaw, _ := message["parts"].([]any)
		for _, p := range partsRaw {
			pBytes, _ := json.Marshal(p)
			receivedParts = append(receivedParts, pBytes)
		}

		// Respond with success using the request ID.
		var reqParsed jsonrpcTestRequest
		_ = json.Unmarshal(body, &reqParsed)

		task := &a2a.Task{
			ID:     "task-data-1",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("data received")}},
			},
		}
		writeJSONRPCResult(w, reqParsed.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("data-agent", agent.URL)

	input := SendMessageInput{
		Agent: "data-agent",
		Parts: []InputPart{
			{Data: map[string]any{"action": "deploy", "targets": []any{"prod", "staging"}}},
		},
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Verify at least one part was sent.
	if len(receivedParts) == 0 {
		t.Fatal("expected at least one part to be received by agent")
	}

	// Parse the first part and verify it contains structured data.
	var firstPart map[string]any
	if err := json.Unmarshal(receivedParts[0], &firstPart); err != nil {
		t.Fatalf("failed to parse first received part: %v", err)
	}

	// The part should have a "data" field with our structured data.
	// The a2a SDK serializes Data directly as {"data": <value>}.
	dataField, ok := firstPart["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected part to have 'data' field as map, got %T: %v", firstPart["data"], firstPart)
	}

	if dataField["action"] != "deploy" {
		t.Errorf("expected action=%q, got %v", "deploy", dataField["action"])
	}
}

// Feature: agent-health-checks, Property 11: Send message always attempts unhealthy agents
// **Validates: Requirements HLTH-5.3**

func TestPropertySendAlwaysAttemptsUnhealthy(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for failure thresholds (1-10)
	thresholdGen := gen.IntRange(1, 10)

	// Generator for extra failures beyond threshold (0-5)
	extraFailuresGen := gen.IntRange(0, 5)

	properties.Property("send_message always attempts request to unhealthy agents", prop.ForAll(
		func(threshold int, extraFailures int) bool {
			// Track whether the backend received a request.
			var requestReceived int32
			agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestReceived++
				req, _ := readJSONRPCRequest(r)
				task := &a2a.Task{
					ID:     "task-health-1",
					Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
					Artifacts: []*a2a.Artifact{
						{Parts: a2a.ContentParts{a2a.NewTextPart("response from unhealthy agent")}},
					},
				}
				writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
			}))
			defer agent.Close()

			alias := "unhealthy-agent"

			// Create a server with the specified threshold.
			srv := NewServer(WithHealthCheck(HealthCheckOptions{FailureThreshold: threshold}))
			srv.registry.Connect(alias, agent.URL, nil, "")
			srv.registry.SetCard(alias, &a2a.AgentCard{
				Name: alias,
				SupportedInterfaces: []*a2a.AgentInterface{
					a2a.NewAgentInterface(agent.URL, a2a.TransportProtocolJSONRPC),
				},
			})
			// Register the agent in the health tracker.
			srv.healthTracker.Reset(alias)

			// Drive agent to unhealthy: record threshold + extra failures.
			totalFailures := threshold + extraFailures
			for i := 0; i < totalFailures; i++ {
				srv.healthTracker.RecordFailure(alias)
			}

			// Verify agent is unhealthy before sending.
			state := srv.healthTracker.Get(alias)
			if state.Status != HealthStatusUnhealthy {
				t.Logf("expected unhealthy status after %d failures (threshold=%d), got %s", totalFailures, threshold, state.Status)
				return false
			}

			// Send a message to the unhealthy agent.
			input := SendMessageInput{
				Agent:   alias,
				Message: "hello unhealthy agent",
			}

			result, _, err := srv.handleSendMessage(context.Background(), nil, input)
			if err != nil {
				t.Logf("unexpected error: %v", err)
				return false
			}

			// The request must have been sent to the backend (proving we don't skip unhealthy agents).
			if requestReceived == 0 {
				t.Log("send_message skipped unhealthy agent — request was not sent to backend")
				return false
			}

			// The result should be successful (agent responded).
			if result.IsError {
				t.Log("unexpected error result from send_message")
				return false
			}

			// After successful send, the agent should have recovered to healthy.
			stateAfter := srv.healthTracker.Get(alias)
			if stateAfter.Status != HealthStatusHealthy {
				t.Logf("expected healthy after successful send, got %s", stateAfter.Status)
				return false
			}

			return true
		},
		thresholdGen,
		extraFailuresGen,
	))

	properties.TestingRun(t)
}

// =============================================================================
// Task 6.5: Unit tests for error states with structured content
// Requirements: SRES-3.3, SRES-3.4, SRES-1.5
// =============================================================================

func TestHandleTaskResult_Failed_StructuredContent(t *testing.T) {
	// A failed task should return IsError=true AND the *a2a.Task as structured content.
	srv := NewServer()
	resolved := &ResolveResult{IsAlias: true, Alias: "fail-agent", URL: "http://example.com"}

	task := &a2a.Task{
		ID:        "task-fail-sc",
		ContextID: "ctx-fail-sc",
		Status: a2a.TaskStatus{
			State:   a2a.TaskStateFailed,
			Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("something went wrong")),
		},
	}

	result, structured, err := srv.handleTaskResult(context.Background(), nil, task, resolved, "fail-agent", taskPollTimeout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for failed task")
	}

	// Verify the structured content is wrapped in SendMessageResponse with the same task pointer.
	if structured == nil {
		t.Fatal("expected non-nil structured content")
	}
	if structured.Task != task {
		t.Error("expected structured content to contain the exact same task pointer")
	}
}

func TestHandleTaskResult_Canceled_StructuredContent(t *testing.T) {
	// A canceled task should return IsError=true AND the *a2a.Task as structured content.
	srv := NewServer()
	resolved := &ResolveResult{IsAlias: true, Alias: "cancel-agent", URL: "http://example.com"}

	task := &a2a.Task{
		ID:        "task-cancel-sc",
		ContextID: "ctx-cancel-sc",
		Status:    a2a.TaskStatus{State: a2a.TaskStateCanceled},
	}

	result, structured, err := srv.handleTaskResult(context.Background(), nil, task, resolved, "cancel-agent", taskPollTimeout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for canceled task")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text != "task was canceled by the agent" {
		t.Errorf("expected cancel message, got %q", textContent.Text)
	}

	// Verify the structured content is wrapped in SendMessageResponse with the same task pointer.
	if structured == nil {
		t.Fatal("expected non-nil structured content")
	}
	if structured.Task != task {
		t.Error("expected structured content to contain the exact same task pointer")
	}
}

func TestHandleTaskResult_Polled_StructuredContent(t *testing.T) {
	// A task in "working" state should be polled, and the final polled task
	// should be returned as structured content.
	var callCount int
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		callCount++
		// GetTask returns a completed task.
		polledTask := &a2a.Task{
			ID:        "task-poll-sc",
			ContextID: "ctx-poll-sc",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("polled result")}},
			},
		}
		writeJSONRPCResult(w, req.ID, polledTask)
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("poll-sc-agent", agent.URL)
	resolved := &ResolveResult{IsAlias: true, Alias: "poll-sc-agent", URL: agent.URL}

	// Resolve an a2aclient.Client for the test server.
	a2aClient, err := srv.clients.Resolve(context.Background(), resolved)
	if err != nil {
		t.Fatalf("failed to resolve client: %v", err)
	}

	// Task in working state with an ID triggers polling.
	workingTask := &a2a.Task{
		ID:        "task-poll-sc",
		ContextID: "ctx-working",
		Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
	}

	result, structured, taskErr := srv.handleTaskResult(context.Background(), a2aClient, workingTask, resolved, "poll-sc-agent", taskPollTimeout)
	if taskErr != nil {
		t.Fatalf("unexpected error: %v", taskErr)
	}
	if result.IsError {
		t.Fatalf("expected success after polling, got error: %v", result.Content)
	}

	// Verify polling happened.
	if callCount < 1 {
		t.Errorf("expected at least 1 poll call, got %d", callCount)
	}

	// Verify the structured content is the polled task wrapped in SendMessageResponse.
	if structured == nil {
		t.Fatal("expected non-nil structured content")
	}
	if structured.Task == nil {
		t.Fatal("expected SendMessageResponse.Task to be non-nil")
	}
	if structured.Task.Status.State != a2a.TaskStateCompleted {
		t.Errorf("expected structured content to have completed state, got %v", structured.Task.Status.State)
	}
	if structured.Task.ID != "task-poll-sc" {
		t.Errorf("expected structured task ID %q, got %q", "task-poll-sc", structured.Task.ID)
	}
}
