package tool

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/nisimpson/a2a-gateway-mcp/registry"
)

func newGetTaskTool(reg *mockRegistry, clientResolver *mockClientResolver) *GetTaskTool {
	return &GetTaskTool{
		AgentRegistry:     reg,
		A2AClientResolver: clientResolver,
	}
}

func TestGetTask_AgentRequired(t *testing.T) {
	g := newGetTaskTool(&mockRegistry{}, &mockClientResolver{})
	result, output, err := g.Handle(context.Background(), nil, &GetTaskInput{})
	if err == nil {
		t.Fatal("expected error for missing agent")
	}
	if result != nil || output != nil {
		t.Fatal("expected nil result and output for validation error")
	}
}

func TestGetTask_TaskIDRequired(t *testing.T) {
	g := newGetTaskTool(&mockRegistry{}, &mockClientResolver{})
	result, output, err := g.Handle(context.Background(), nil, &GetTaskInput{Agent: "test-agent"})
	if err == nil {
		t.Fatal("expected error for missing task_id")
	}
	if result != nil || output != nil {
		t.Fatal("expected nil result and output for validation error")
	}
}

func TestGetTask_Completed(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:        "task-123",
			ContextID: "ctx-abc",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("task result")}},
			},
		}
		writeJSONRPCResult(w, req.ID, task)
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

	g := newGetTaskTool(reg, clientResolver)
	result, output, err := g.Handle(context.Background(), nil, &GetTaskInput{
		Agent:  "test-agent",
		TaskID: "task-123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result for non-error case, got %v", result)
	}
	if output == nil {
		t.Fatal("expected non-nil output")
	}
	task := output.(*a2a.Task)
	if task.ID != "task-123" {
		t.Errorf("expected ID task-123, got %s", task.ID)
	}
	if task.ContextID != "ctx-abc" {
		t.Errorf("expected ContextID ctx-abc, got %s", task.ContextID)
	}
	if task.Status.State != a2a.TaskStateCompleted {
		t.Errorf("expected state completed, got %s", task.Status.State)
	}
}

func TestGetTask_Failed(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID: "task-fail",
			Status: a2a.TaskStatus{
				State:   a2a.TaskStateFailed,
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("something went wrong")),
			},
		}
		writeJSONRPCResult(w, req.ID, task)
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

	g := newGetTaskTool(reg, clientResolver)
	result, output, err := g.Handle(context.Background(), nil, &GetTaskInput{
		Agent:  "test-agent",
		TaskID: "task-fail",
	})
	if err == nil {
		t.Fatal("expected error for failed task")
	}
	if result != nil {
		t.Fatalf("expected nil result when error is returned")
	}
	if output == nil {
		t.Fatal("expected non-nil output")
	}
	task := output.(*a2a.Task)
	if task.ID != "task-fail" {
		t.Errorf("expected ID task-fail, got %s", task.ID)
	}
	if task.Status.State != a2a.TaskStateFailed {
		t.Errorf("expected state failed, got %s", task.Status.State)
	}
	if err.Error() != "something went wrong" {
		t.Errorf("expected error message 'something went wrong', got %s", err.Error())
	}
}

func TestGetTask_Canceled(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:        "task-cancel",
			ContextID: "ctx-xyz",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCanceled},
		}
		writeJSONRPCResult(w, req.ID, task)
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

	g := newGetTaskTool(reg, clientResolver)
	result, output, err := g.Handle(context.Background(), nil, &GetTaskInput{
		Agent:  "test-agent",
		TaskID: "task-cancel",
	})
	if err == nil {
		t.Fatal("expected error for canceled task")
	}
	if result != nil {
		t.Fatalf("expected nil result when error is returned")
	}
	if output == nil {
		t.Fatal("expected non-nil output")
	}
	task := output.(*a2a.Task)
	if task.ID != "task-cancel" {
		t.Errorf("expected ID task-cancel, got %s", task.ID)
	}
	if task.ContextID != "ctx-xyz" {
		t.Errorf("expected ContextID ctx-xyz, got %s", task.ContextID)
	}
	if task.Status.State != a2a.TaskStateCanceled {
		t.Errorf("expected state canceled, got %s", task.Status.State)
	}
	if err.Error() != "task was canceled" {
		t.Errorf("expected error message 'task was canceled', got %s", err.Error())
	}
}
