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
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func newSendTool(reg *mockRegistry, clientResolver *mockClientResolver) *SendMessageTool {
	return &SendMessageTool{
		AgentRegistry:      reg,
		A2AClientResolver:  clientResolver,
		ContextStore:       newMockContextStore(),
		CallerCardInjector: &mockCallerCardInjector{},
		HealthTracker:      &mockHealthTracker{},
		HistoryRecorder:    &mockHistoryRecorder{},
		RateLimiter:        &mockRateLimiter{},
		StreamTimeout:      5 * time.Second,
		EffectivePollTimeout: func(_ *int) time.Duration { return 10 * time.Second },
	}
}

func TestSendMessage_AgentRequired(t *testing.T) {
	s := newSendTool(&mockRegistry{}, &mockClientResolver{})
	result, out, err := s.Handle(context.Background(), nil, &SendMessageInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	if out != nil {
		t.Fatal("expected nil structured output")
	}
	assertTextContains(t, result, "agent identifier is required")
}

func TestSendMessage_MessageOrPartsRequired(t *testing.T) {
	s := newSendTool(&mockRegistry{}, &mockClientResolver{})
	result, _, err := s.Handle(context.Background(), nil, &SendMessageInput{Agent: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	assertTextContains(t, result, "either 'message' or 'parts' is required")
}

func TestSendMessage_InvalidAgent(t *testing.T) {
	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*ResolveResult, error) {
			return nil, fmt.Errorf("agent not found")
		},
	}
	s := newSendTool(reg, &mockClientResolver{})
	result, _, err := s.Handle(context.Background(), nil, &SendMessageInput{
		Agent:   "nonexistent",
		Message: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	assertTextContains(t, result, "agent not found")
}

func TestSendMessage_RateLimited(t *testing.T) {
	reg := &mockRegistry{}
	s := newSendTool(reg, &mockClientResolver{})
	s.RateLimiter = &mockRateLimiter{
		CheckRateLimitFn: func(alias string) *mcp.CallToolResult {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "rate limited"}},
			}
		},
	}

	result, _, err := s.Handle(context.Background(), nil, &SendMessageInput{
		Agent:   "test-agent",
		Message: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	assertTextContains(t, result, "rate limited")
}

func TestSendMessage_DirectPath_MessageResponse(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("hello back"))
		msg.ContextID = "ctx-123"
		writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
	}))
	defer agent.Close()

	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*ResolveResult, error) {
			return &ResolveResult{URL: agent.URL, IsAlias: true, Alias: identifier}, nil
		},
	}

	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}

	s := newSendTool(reg, clientResolver)
	result, out, err := s.Handle(context.Background(), nil, &SendMessageInput{
		Agent:   "test-agent",
		Message: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}
	if out == nil {
		t.Fatal("expected structured output")
	}

	// Verify structured output wraps the message.
	structured, ok := out.(*SendMessageOutput)
	if !ok {
		t.Fatalf("expected *SendMessageOutput, got %T", out)
	}
	if structured.Message == nil {
		t.Fatal("expected Message in structured output")
	}
	if structured.Message.ContextID != "ctx-123" {
		t.Errorf("expected context_id ctx-123, got %s", structured.Message.ContextID)
	}
}

func TestSendMessage_DirectPath_TaskCompleted(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:        "task-1",
			ContextID: "ctx-456",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("result text")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*ResolveResult, error) {
			return &ResolveResult{URL: agent.URL, IsAlias: true, Alias: identifier}, nil
		},
	}
	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}

	s := newSendTool(reg, clientResolver)
	result, out, err := s.Handle(context.Background(), nil, &SendMessageInput{
		Agent:   "test-agent",
		Message: "do something",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}

	structured, ok := out.(*SendMessageOutput)
	if !ok {
		t.Fatalf("expected *SendMessageOutput, got %T", out)
	}
	if structured.Task == nil {
		t.Fatal("expected Task in structured output")
	}
	if structured.Task.ID != "task-1" {
		t.Errorf("expected task ID task-1, got %s", structured.Task.ID)
	}

	// Verify content has the response text.
	assertTextContains(t, result, "result text")
}

func TestSendMessage_DirectPath_TaskFailed(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:     "task-fail",
			Status: a2a.TaskStatus{
				State:   a2a.TaskStateFailed,
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("something broke")),
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*ResolveResult, error) {
			return &ResolveResult{URL: agent.URL, IsAlias: true, Alias: identifier}, nil
		},
	}
	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}

	s := newSendTool(reg, clientResolver)
	result, out, err := s.Handle(context.Background(), nil, &SendMessageInput{
		Agent:   "test-agent",
		Message: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for failed task")
	}

	structured, ok := out.(*SendMessageOutput)
	if !ok {
		t.Fatalf("expected *SendMessageOutput, got %T", out)
	}
	if structured.Task == nil {
		t.Fatal("expected Task in structured output for failed state")
	}
	assertTextContains(t, result, "something broke")
}

func TestSendMessage_ContextStoreUsed(t *testing.T) {
	var capturedContextID string
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		// Parse the message to check context_id was sent.
		var params struct {
			Message struct {
				ContextID string `json:"contextId"`
			} `json:"message"`
		}
		_ = json.Unmarshal(req.Params, &params)
		capturedContextID = params.Message.ContextID

		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("ok"))
		writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
	}))
	defer agent.Close()

	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*ResolveResult, error) {
			return &ResolveResult{URL: agent.URL, IsAlias: true, Alias: identifier}, nil
		},
	}
	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}

	s := newSendTool(reg, clientResolver)
	// Pre-seed the context store.
	s.ContextStore.Set("test-agent", "stored-ctx-id")

	_, _, err := s.Handle(context.Background(), nil, &SendMessageInput{
		Agent:   "test-agent",
		Message: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}

	if capturedContextID != "stored-ctx-id" {
		t.Errorf("expected stored context_id to be sent, got %q", capturedContextID)
	}
}

func TestSendMessage_HealthRecordedOnSuccess(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("ok"))
		writeJSONRPCResult(w, req.ID, jsonrpcMessageResult(msg))
	}))
	defer agent.Close()

	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*ResolveResult, error) {
			return &ResolveResult{URL: agent.URL, IsAlias: true, Alias: identifier}, nil
		},
	}
	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}

	var successCalled bool
	health := &mockHealthTracker{
		RecordSuccessFn: func(alias string) { successCalled = true },
	}

	s := newSendTool(reg, clientResolver)
	s.HealthTracker = health

	_, _, err := s.Handle(context.Background(), nil, &SendMessageInput{
		Agent:   "test-agent",
		Message: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !successCalled {
		t.Error("expected RecordSuccess to be called")
	}
}

// --- Helpers ---

func assertTextContains(t *testing.T, result *mcp.CallToolResult, substr string) {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatalf("expected content, got none")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if !contains(tc.Text, substr) {
		t.Errorf("expected text to contain %q, got %q", substr, tc.Text)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || findSubstring(s, substr))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
