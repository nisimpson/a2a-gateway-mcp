package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHandleGetTask_CompletedTask(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)

		// Verify the params contain the task ID.
		var params a2a.GetTaskRequest
		if err := json.Unmarshal(req.Params, &params); err != nil {
			t.Fatalf("failed to decode params: %v", err)
		}
		if string(params.ID) != "task-123" {
			t.Errorf("expected task ID %q, got %q", "task-123", params.ID)
		}

		task := &a2a.Task{
			ID:        "task-123",
			ContextID: "ctx-123",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("Hello from completed task")}},
			},
		}
		writeJSONRPCResult(w, req.ID, task)
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("test-agent", agent.URL)

	input := GetTaskInput{
		Agent:  "test-agent",
		TaskID: "task-123",
	}

	result, _, err := srv.handleGetTask(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Should have text content + task_id + context_id.
	if len(result.Content) < 3 {
		t.Fatalf("expected at least 3 content items, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected first content to be TextContent")
	}
	if textContent.Text != "Hello from completed task" {
		t.Errorf("expected %q, got %q", "Hello from completed task", textContent.Text)
	}

	// Verify task_id is included.
	taskIDContent, ok := result.Content[1].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected second content to be TextContent")
	}
	if taskIDContent.Text != "task_id:task-123" {
		t.Errorf("expected %q, got %q", "task_id:task-123", taskIDContent.Text)
	}

	// Verify context_id is included.
	ctxContent, ok := result.Content[2].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected third content to be TextContent")
	}
	if ctxContent.Text != "context_id:ctx-123" {
		t.Errorf("expected %q, got %q", "context_id:ctx-123", ctxContent.Text)
	}
}

func TestHandleGetTask_FailedTask(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID: "task-456",
			Status: a2a.TaskStatus{
				State:   a2a.TaskStateFailed,
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("processing error occurred")),
			},
		}
		writeJSONRPCResult(w, req.ID, task)
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("fail-agent", agent.URL)

	input := GetTaskInput{
		Agent:  "fail-agent",
		TaskID: "task-456",
	}

	result, _, err := srv.handleGetTask(context.Background(), nil, input)
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
	if textContent.Text != "processing error occurred" {
		t.Errorf("expected %q, got %q", "processing error occurred", textContent.Text)
	}
}

func TestHandleGetTask_InputRequired(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:        "task-789",
			ContextID: "ctx-789",
			Status: a2a.TaskStatus{
				State:   a2a.TaskStateInputRequired,
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Please provide your API key")),
			},
		}
		writeJSONRPCResult(w, req.ID, task)
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("input-agent", agent.URL)

	input := GetTaskInput{
		Agent:  "input-agent",
		TaskID: "task-789",
	}

	result, _, err := srv.handleGetTask(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected success for input-required state")
	}

	// Should have: status message, state indicator, task_id, context_id.
	if len(result.Content) < 4 {
		t.Fatalf("expected at least 4 content items, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected first content to be TextContent")
	}
	if textContent.Text != "Please provide your API key" {
		t.Errorf("expected %q, got %q", "Please provide your API key", textContent.Text)
	}

	stateContent, ok := result.Content[1].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected second content to be TextContent")
	}
	if stateContent.Text != "state:input-required" {
		t.Errorf("expected %q, got %q", "state:input-required", stateContent.Text)
	}

	taskIDContent, ok := result.Content[2].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected third content to be TextContent")
	}
	if taskIDContent.Text != "task_id:task-789" {
		t.Errorf("expected %q, got %q", "task_id:task-789", taskIDContent.Text)
	}

	ctxContent, ok := result.Content[3].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected fourth content to be TextContent")
	}
	if ctxContent.Text != "context_id:ctx-789" {
		t.Errorf("expected %q, got %q", "context_id:ctx-789", ctxContent.Text)
	}
}

func TestHandleGetTask_TaskNotFound(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		writeJSONRPCError(w, req.ID, -32001, "task not found")
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("notfound-agent", agent.URL)

	input := GetTaskInput{
		Agent:  "notfound-agent",
		TaskID: "nonexistent-task",
	}

	result, _, err := srv.handleGetTask(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for task not found")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text != "task not found" {
		t.Errorf("expected %q, got %q", "task not found", textContent.Text)
	}
}

func TestHandleGetTask_UnreachableAgent(t *testing.T) {
	srv := NewServer()
	// Register an agent pointing to a non-existent server.
	srv.registry.Connect("dead-agent", "http://127.0.0.1:1", nil, "")

	input := GetTaskInput{
		Agent:  "dead-agent",
		TaskID: "task-abc",
	}

	result, _, err := srv.handleGetTask(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for unreachable agent")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text == "" {
		t.Error("expected non-empty error message for unreachable agent")
	}
}

func TestHandleGetTask_MissingTaskID(t *testing.T) {
	srv := NewServer()

	input := GetTaskInput{
		Agent:  "some-agent",
		TaskID: "",
	}

	result, _, err := srv.handleGetTask(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing task_id")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text != "task_id is required" {
		t.Errorf("expected %q, got %q", "task_id is required", textContent.Text)
	}
}

func TestHandleGetTask_MissingAgent(t *testing.T) {
	srv := NewServer()

	input := GetTaskInput{
		Agent:  "",
		TaskID: "task-123",
	}

	result, _, err := srv.handleGetTask(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing agent")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text != "agent identifier is required" {
		t.Errorf("expected %q, got %q", "agent identifier is required", textContent.Text)
	}
}

func TestHandleGetTask_HTTPError(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		writeJSONRPCError(w, req.ID, -32603, "internal server error")
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("error-agent", agent.URL)

	input := GetTaskInput{
		Agent:  "error-agent",
		TaskID: "task-123",
	}

	result, _, err := srv.handleGetTask(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for JSON-RPC error response")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text == "" {
		t.Error("expected non-empty error message")
	}
}

func TestHandleGetTask_AuthRequired(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:        "task-auth",
			ContextID: "ctx-auth",
			Status: a2a.TaskStatus{
				State:   a2a.TaskStateAuthRequired,
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("OAuth token needed")),
			},
		}
		writeJSONRPCResult(w, req.ID, task)
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("auth-agent", agent.URL)

	input := GetTaskInput{
		Agent:  "auth-agent",
		TaskID: "task-auth",
	}

	result, _, err := srv.handleGetTask(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected success for auth-required state")
	}

	// Should have: status message, state indicator, task_id, context_id.
	if len(result.Content) < 4 {
		t.Fatalf("expected at least 4 content items, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected first content to be TextContent")
	}
	if textContent.Text != "OAuth token needed" {
		t.Errorf("expected %q, got %q", "OAuth token needed", textContent.Text)
	}

	stateContent, ok := result.Content[1].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected second content to be TextContent")
	}
	if stateContent.Text != "state:auth-required" {
		t.Errorf("expected %q, got %q", "state:auth-required", stateContent.Text)
	}
}

func TestHandleGetTask_WorkingState(t *testing.T) {
	// For non-terminal states like "working", get_task just returns the current state.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:     "task-working",
			Status: a2a.TaskStatus{State: a2a.TaskStateWorking},
		}
		writeJSONRPCResult(w, req.ID, task)
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("working-agent", agent.URL)

	input := GetTaskInput{
		Agent:  "working-agent",
		TaskID: "task-working",
	}

	result, _, err := srv.handleGetTask(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected success for working state")
	}

	// Should include task_id.
	found := false
	for _, c := range result.Content {
		tc, ok := c.(*mcp.TextContent)
		if ok && tc.Text == "task_id:task-working" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected task_id:task-working in content")
	}
}

func TestHandleGetTask_URLBased(t *testing.T) {
	// Verify that get_task works with a direct URL (not alias).
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:     "task-url",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("result via URL")}},
			},
		}
		writeJSONRPCResult(w, req.ID, task)
	}))
	defer agent.Close()

	srv := NewServer()

	input := GetTaskInput{
		Agent:  agent.URL,
		TaskID: "task-url",
	}

	result, _, err := srv.handleGetTask(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected first content to be TextContent")
	}
	if textContent.Text != "result via URL" {
		t.Errorf("expected %q, got %q", "result via URL", textContent.Text)
	}
}
