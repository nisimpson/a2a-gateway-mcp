package tool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
)

func newCancelTaskTool(reg *mockRegistry, clientResolver *mockClientResolver) *CancelTaskTool {
	return &CancelTaskTool{
		AgentRegistry:     reg,
		A2AClientResolver: clientResolver,
	}
}

func TestCancelTask_AgentRequired(t *testing.T) {
	c := newCancelTaskTool(&mockRegistry{}, &mockClientResolver{})
	result, _, err := c.Handle(context.Background(), nil, &CancelTaskInput{})
	if err == nil {
		t.Fatal("expected error")
	}
	if result != nil {
		t.Fatal("expected nil result for validation error")
	}
	if err.Error() != "agent identifier is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCancelTask_TaskIDRequired(t *testing.T) {
	c := newCancelTaskTool(&mockRegistry{}, &mockClientResolver{})
	result, _, err := c.Handle(context.Background(), nil, &CancelTaskInput{Agent: "test-agent"})
	if err == nil {
		t.Fatal("expected error")
	}
	if result != nil {
		t.Fatal("expected nil result for validation error")
	}
	if err.Error() != "task_id is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCancelTask_Success(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:     "task-abc",
			Status: a2a.TaskStatus{State: a2a.TaskStateCanceled},
		}
		writeJSONRPCResult(w, req.ID, task)
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

	c := newCancelTaskTool(reg, clientResolver)
	result, out, err := c.Handle(context.Background(), nil, &CancelTaskInput{
		Agent:  "test-agent",
		TaskID: "task-abc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatalf("unexpected result for success: %v", result)
	}
	if out == nil {
		t.Fatal("expected structured output")
	}
}

func TestCancelTask_NotCancelable(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		// Return a JSON-RPC error for non-cancelable task
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"error": map[string]any{
				"code":    -32600,
				"message": "task is not cancelable",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
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

	c := newCancelTaskTool(reg, clientResolver)
	result, out, err := c.Handle(context.Background(), nil, &CancelTaskInput{
		Agent:  "test-agent",
		TaskID: "task-xyz",
	})
	if err == nil {
		t.Fatal("expected error for non-cancelable task")
	}
	if result != nil {
		t.Fatalf("unexpected result: %v", result)
	}
	if out != nil {
		t.Fatalf("unexpected output: %v", out)
	}
	if err.Error() != "task is not cancelable" {
		t.Fatalf("unexpected error: %v", err)
	}
}
