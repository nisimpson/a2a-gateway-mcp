package tool

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
)

func newGetTaskTool(reg *mockRegistry, clientResolver *mockClientResolver) *GetTaskTool {
	return &GetTaskTool{
		AgentRegistry:     reg,
		A2AClientResolver: clientResolver,
	}
}

func TestGetTask_AgentRequired(t *testing.T) {
	g := newGetTaskTool(&mockRegistry{}, &mockClientResolver{})
	result, _, err := g.Handle(context.Background(), nil, &GetTaskInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	assertTextContains(t, result, "agent identifier is required")
}

func TestGetTask_TaskIDRequired(t *testing.T) {
	g := newGetTaskTool(&mockRegistry{}, &mockClientResolver{})
	result, _, err := g.Handle(context.Background(), nil, &GetTaskInput{Agent: "test-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	assertTextContains(t, result, "task_id is required")
}

func TestGetTask_Completed(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:     "task-123",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("task result")}},
			},
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

	g := newGetTaskTool(reg, clientResolver)
	result, _, err := g.Handle(context.Background(), nil, &GetTaskInput{
		Agent:  "test-agent",
		TaskID: "task-123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}
	assertTextContains(t, result, "task result")
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
		ResolveAgentFn: func(identifier string) (*ResolveResult, error) {
			return &ResolveResult{URL: agent.URL, IsAlias: true, Alias: identifier}, nil
		},
	}
	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
	}

	g := newGetTaskTool(reg, clientResolver)
	result, _, err := g.Handle(context.Background(), nil, &GetTaskInput{
		Agent:  "test-agent",
		TaskID: "task-fail",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for failed task")
	}
	assertTextContains(t, result, "something went wrong")
}

func TestGetTask_Canceled(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:     "task-cancel",
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

	g := newGetTaskTool(reg, clientResolver)
	result, _, err := g.Handle(context.Background(), nil, &GetTaskInput{
		Agent:  "test-agent",
		TaskID: "task-cancel",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for canceled task")
	}
	assertTextContains(t, result, "task was canceled")
}
