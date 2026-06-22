package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/nisimpson/a2a-gateway-mcp/registry"
)

func newSendToolWithInbox(reg *mockRegistry, clientResolver *mockClientResolver, inbox *mockInbox) *SendMessageTool {
	return &SendMessageTool{
		AgentRegistry:          reg,
		A2AClientResolver:      clientResolver,
		ContextStore:           newMockContextStore(),
		CallerCardInjector:     &mockCallerCardInjector{},
		HealthTracker:          &mockHealthTracker{},
		HistoryRecorder:        &mockHistoryRecorder{},
		RateLimiter:            &mockRateLimiter{},
		EffectiveStreamTimeout: effectiveStreamTimeout(5 * time.Second),
		EffectivePollTimeout:   func(_ *int) time.Duration { return 10 * time.Second },
		Inbox:                  inbox,
	}
}

func TestSendMessage_Async_ReturnsImmediately(t *testing.T) {
	// Agent that will block for a while to prove we don't wait for it.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:        "task-1",
			ContextID: "ctx-1",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*registry.ResolveResult, error) {
			return &registry.ResolveResult{URL: agent.URL, IsAlias: true, Alias: identifier}, nil
		},
	}
	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}

	inbox := &mockInbox{}
	s := newSendToolWithInbox(reg, clientResolver, inbox)

	start := time.Now()
	result, out, err := s.Handle(context.Background(), nil, &SendMessageInput{
		Agent:   "test-agent",
		Message: "hello",
		Async:   boolPtr(true),
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	// Async path returns nil CallToolResult and structured output with Async field.
	if result != nil {
		t.Fatal("expected nil CallToolResult for async dispatch")
	}
	if out == nil {
		t.Fatal("expected non-nil structured output for async dispatch")
	}
	if out.Async == nil {
		t.Fatal("expected non-nil Async field in structured output")
	}

	// Should return in well under the 500ms agent delay.
	if elapsed > 200*time.Millisecond {
		t.Errorf("async should return immediately, took %v", elapsed)
	}

	// Verify the structured async output.
	if out.Async.Alias != "test-agent" {
		t.Errorf("expected alias 'test-agent', got %q", out.Async.Alias)
	}
	if out.Async.Status != "dispatched" {
		t.Errorf("expected status 'dispatched', got %q", out.Async.Status)
	}
}

func TestSendMessage_Async_InboxEntryDeposited(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:        "task-async",
			ContextID: "ctx-async",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("async result")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*registry.ResolveResult, error) {
			return &registry.ResolveResult{URL: agent.URL, IsAlias: true, Alias: identifier}, nil
		},
	}
	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}

	inbox := &mockInbox{}
	s := newSendToolWithInbox(reg, clientResolver, inbox)

	_, _, err := s.Handle(context.Background(), nil, &SendMessageInput{
		Agent:   "test-agent",
		Message: "hello",
		Async:   boolPtr(true),
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Wait for the background goroutine to complete.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		entries := inbox.Peek(registry.InboxPeekFilter{Alias: "test-agent"})
		if len(entries) > 0 {
			entry := entries[0]
			if entry.Alias != "test-agent" {
				t.Errorf("expected alias 'test-agent', got %q", entry.Alias)
			}
			if entry.TaskID != "task-async" {
				t.Errorf("expected task ID 'task-async', got %q", entry.TaskID)
			}
			if entry.ContextID != "ctx-async" {
				t.Errorf("expected context ID 'ctx-async', got %q", entry.ContextID)
			}
			if entry.State != string(a2a.TaskStateCompleted) {
				t.Errorf("expected state %q, got %q", a2a.TaskStateCompleted, entry.State)
			}
			if entry.Task == nil {
				t.Error("expected non-nil Task in entry")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out waiting for inbox entry to be deposited")
}

func TestSendMessage_Async_MessageResponseDeposited(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("async msg"))
		msg.ContextID = "msg-ctx"
		writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
	}))
	defer agent.Close()

	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*registry.ResolveResult, error) {
			return &registry.ResolveResult{URL: agent.URL, IsAlias: true, Alias: identifier}, nil
		},
	}
	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}

	inbox := &mockInbox{}
	s := newSendToolWithInbox(reg, clientResolver, inbox)

	_, _, err := s.Handle(context.Background(), nil, &SendMessageInput{
		Agent:   "test-agent",
		Message: "hello",
		Async:   boolPtr(true),
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Wait for the background goroutine to complete.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		entries := inbox.Peek(registry.InboxPeekFilter{Alias: "test-agent"})
		if len(entries) > 0 {
			entry := entries[0]
			if entry.State != "completed" {
				t.Errorf("expected state 'completed', got %q", entry.State)
			}
			if entry.ContextID != "msg-ctx" {
				t.Errorf("expected context ID 'msg-ctx', got %q", entry.ContextID)
			}
			if entry.Message == nil {
				t.Error("expected non-nil Message in entry")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out waiting for inbox entry to be deposited")
}

func TestSendMessage_Async_ErrorEntryDeposited(t *testing.T) {
	// Agent returns an error.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"error":   map[string]any{"code": -32000, "message": "agent error"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer agent.Close()

	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*registry.ResolveResult, error) {
			return &registry.ResolveResult{URL: agent.URL, IsAlias: true, Alias: identifier}, nil
		},
	}
	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}

	inbox := &mockInbox{}
	s := newSendToolWithInbox(reg, clientResolver, inbox)

	_, _, err := s.Handle(context.Background(), nil, &SendMessageInput{
		Agent:   "test-agent",
		Message: "hello",
		Async:   boolPtr(true),
	})
	if err != nil {
		t.Fatalf("expected no error from async dispatch, got: %v", err)
	}

	// Wait for the background goroutine to deposit error entry.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		entries := inbox.Peek(registry.InboxPeekFilter{Alias: "test-agent"})
		if len(entries) > 0 {
			entry := entries[0]
			if entry.State != "error" {
				t.Errorf("expected state 'error', got %q", entry.State)
			}
			if entry.Error == "" {
				t.Error("expected non-empty Error field in error entry")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out waiting for error inbox entry")
}

func TestSendMessage_Async_ValidationErrorReturnsSynchronously(t *testing.T) {
	inbox := &mockInbox{}

	tests := []struct {
		name    string
		input   *SendMessageInput
		wantErr string
	}{
		{
			name:    "empty agent",
			input:   &SendMessageInput{Message: "hello", Async: boolPtr(true)},
			wantErr: "agent identifier is required",
		},
		{
			name:    "no message or parts",
			input:   &SendMessageInput{Agent: "test-agent", Async: boolPtr(true)},
			wantErr: "either 'message' or 'parts' is required",
		},
		{
			name:    "agent not found",
			input:   &SendMessageInput{Agent: "nonexistent", Message: "hi", Async: boolPtr(true)},
			wantErr: "agent not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := &mockRegistry{
				ResolveAgentFn: func(identifier string) (*registry.ResolveResult, error) {
					return nil, fmt.Errorf("agent not found")
				},
			}
			s := newSendToolWithInbox(reg, &mockClientResolver{}, inbox)

			result, _, err := s.Handle(context.Background(), nil, tt.input)
			if err == nil {
				t.Fatal("expected synchronous error")
			}
			if result != nil {
				t.Fatal("expected nil result for validation error")
			}
			if err.Error() != tt.wantErr {
				t.Errorf("expected error %q, got %q", tt.wantErr, err.Error())
			}

			// Verify nothing was deposited in inbox.
			entries := inbox.Peek(registry.InboxPeekFilter{})
			if len(entries) != 0 {
				t.Errorf("expected no inbox entries on validation error, got %d", len(entries))
			}
		})
	}
}

func TestSendMessage_Async_DoesNotUpdateContextStore(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:        "task-ctx",
			ContextID: "new-ctx-id",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*registry.ResolveResult, error) {
			return &registry.ResolveResult{URL: agent.URL, IsAlias: true, Alias: identifier}, nil
		},
	}
	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}

	inbox := &mockInbox{}
	s := newSendToolWithInbox(reg, clientResolver, inbox)
	ctxStore := s.ContextStore.(*mockContextStore)

	_, _, err := s.Handle(context.Background(), nil, &SendMessageInput{
		Agent:   "test-agent",
		Message: "hello",
		Async:   boolPtr(true),
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Wait for background to finish.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		entries := inbox.Peek(registry.InboxPeekFilter{Alias: "test-agent"})
		if len(entries) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Context store should NOT have been updated by async path.
	if ctxStore.Get("test-agent") != "" {
		t.Errorf("async path should not update context store, got %q", ctxStore.Get("test-agent"))
	}
}

func TestSendMessage_Async_InputRequiredDeposited(t *testing.T) {
	// Agent returns a task in input-required state (e.g., needs user clarification).
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:        "task-ir",
			ContextID: "ctx-ir",
			Status:    a2a.TaskStatus{State: a2a.TaskStateInputRequired},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*registry.ResolveResult, error) {
			return &registry.ResolveResult{URL: agent.URL, IsAlias: true, Alias: identifier}, nil
		},
	}
	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}

	inbox := &mockInbox{}
	s := newSendToolWithInbox(reg, clientResolver, inbox)

	_, _, err := s.Handle(context.Background(), nil, &SendMessageInput{
		Agent:   "test-agent",
		Message: "hello",
		Async:   boolPtr(true),
	})
	if err != nil {
		t.Fatalf("expected no error from async dispatch, got: %v", err)
	}

	// Wait for the background goroutine to deposit the input-required entry.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		entries := inbox.Peek(registry.InboxPeekFilter{Alias: "test-agent"})
		if len(entries) > 0 {
			entry := entries[0]
			// AINB-7.1: State must be "input-required"
			if entry.State != string(a2a.TaskStateInputRequired) {
				t.Errorf("expected state %q, got %q", a2a.TaskStateInputRequired, entry.State)
			}
			// AINB-7.2: TaskID must be present for follow-up
			if entry.TaskID != "task-ir" {
				t.Errorf("expected task ID 'task-ir', got %q", entry.TaskID)
			}
			// AINB-7.2: ContextID must be present for follow-up
			if entry.ContextID != "ctx-ir" {
				t.Errorf("expected context ID 'ctx-ir', got %q", entry.ContextID)
			}
			// Task payload must be included
			if entry.Task == nil {
				t.Error("expected non-nil Task in inbox entry")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out waiting for input-required inbox entry to be deposited")
}

func TestSendMessage_SyncPath_UnchangedWithAsyncFalse(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("sync response"))
		msg.ContextID = "sync-ctx"
		writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
	}))
	defer agent.Close()

	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*registry.ResolveResult, error) {
			return &registry.ResolveResult{URL: agent.URL, IsAlias: true, Alias: identifier}, nil
		},
	}
	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}

	inbox := &mockInbox{}
	s := newSendToolWithInbox(reg, clientResolver, inbox)

	// Test with async: false
	result, out, err := s.Handle(context.Background(), nil, &SendMessageInput{
		Agent:   "test-agent",
		Message: "hello",
		Async:   boolPtr(false),
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result for sync success: %v", result)
	}
	if out == nil {
		t.Fatal("expected structured output for sync path")
	}
	if out.Message == nil {
		t.Fatal("expected Message in structured output")
	}
	if out.Message.ContextID != "sync-ctx" {
		t.Errorf("expected context_id sync-ctx, got %s", out.Message.ContextID)
	}

	// Verify nothing in inbox.
	entries := inbox.Peek(registry.InboxPeekFilter{})
	if len(entries) != 0 {
		t.Errorf("sync path should not deposit to inbox, got %d entries", len(entries))
	}
}

func TestSendMessage_SyncPath_UnchangedWithAsyncNil(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("sync response"))
		writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
	}))
	defer agent.Close()

	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*registry.ResolveResult, error) {
			return &registry.ResolveResult{URL: agent.URL, IsAlias: true, Alias: identifier}, nil
		},
	}
	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}

	inbox := &mockInbox{}
	s := newSendToolWithInbox(reg, clientResolver, inbox)

	// Test with async: nil (not set)
	result, out, err := s.Handle(context.Background(), nil, &SendMessageInput{
		Agent:   "test-agent",
		Message: "hello",
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result for sync success: %v", result)
	}
	if out == nil {
		t.Fatal("expected structured output for sync path")
	}
	if out.Message == nil {
		t.Fatal("expected Message in structured output")
	}

	// Verify nothing in inbox.
	entries := inbox.Peek(registry.InboxPeekFilter{})
	if len(entries) != 0 {
		t.Errorf("sync path should not deposit to inbox, got %d entries", len(entries))
	}
}
