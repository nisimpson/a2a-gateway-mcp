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

func TestHandleCancelTask_Success(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)

		// Verify the params contain the task ID.
		var params a2a.CancelTaskRequest
		if err := json.Unmarshal(req.Params, &params); err != nil {
			t.Fatalf("failed to decode params: %v", err)
		}
		if string(params.ID) != "task-123" {
			t.Errorf("expected task ID %q, got %q", "task-123", params.ID)
		}

		task := &a2a.Task{
			ID:     "task-123",
			Status: a2a.TaskStatus{State: a2a.TaskStateCanceled},
		}
		writeJSONRPCResult(w, req.ID, task)
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("cancel-agent", agent.URL)

	input := CancelTaskInput{
		Agent:  "cancel-agent",
		TaskID: "task-123",
	}

	result, _, err := srv.handleCancelTask(context.Background(), nil, input)
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
		t.Fatal("expected first content to be TextContent")
	}
	if textContent.Text != "Task task-123 has been canceled" {
		t.Errorf("expected %q, got %q", "Task task-123 has been canceled", textContent.Text)
	}
}

func TestHandleCancelTask_TaskNotCancelable(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		writeJSONRPCError(w, req.ID, -32002, "task is not cancelable")
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("notcancelable-agent", agent.URL)

	input := CancelTaskInput{
		Agent:  "notcancelable-agent",
		TaskID: "task-456",
	}

	result, _, err := srv.handleCancelTask(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for task not cancelable")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text != "task is not cancelable" {
		t.Errorf("expected %q, got %q", "task is not cancelable", textContent.Text)
	}
}

func TestHandleCancelTask_TaskNotFound(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		writeJSONRPCError(w, req.ID, -32001, "task not found")
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("notfound-cancel-agent", agent.URL)

	input := CancelTaskInput{
		Agent:  "notfound-cancel-agent",
		TaskID: "nonexistent-task",
	}

	result, _, err := srv.handleCancelTask(context.Background(), nil, input)
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

func TestHandleCancelTask_UnreachableAgent(t *testing.T) {
	srv := NewServer()
	// Register an agent pointing to a non-existent server.
	srv.registry.Connect("dead-cancel-agent", "http://127.0.0.1:1", nil, "")

	input := CancelTaskInput{
		Agent:  "dead-cancel-agent",
		TaskID: "task-abc",
	}

	result, _, err := srv.handleCancelTask(context.Background(), nil, input)
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

func TestHandleCancelTask_MissingTaskID(t *testing.T) {
	srv := NewServer()

	input := CancelTaskInput{
		Agent:  "some-agent",
		TaskID: "",
	}

	result, _, err := srv.handleCancelTask(context.Background(), nil, input)
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

func TestHandleCancelTask_MissingAgent(t *testing.T) {
	srv := NewServer()

	input := CancelTaskInput{
		Agent:  "",
		TaskID: "task-123",
	}

	result, _, err := srv.handleCancelTask(context.Background(), nil, input)
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

func TestHandleCancelTask_HTTPError(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		writeJSONRPCError(w, req.ID, -32603, "internal server error")
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("error-cancel-agent", agent.URL)

	input := CancelTaskInput{
		Agent:  "error-cancel-agent",
		TaskID: "task-123",
	}

	result, _, err := srv.handleCancelTask(context.Background(), nil, input)
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

func TestHandleCancelTask_URLBased(t *testing.T) {
	// Verify that cancel_task works with a direct URL (not alias).
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		task := &a2a.Task{
			ID:     "task-url",
			Status: a2a.TaskStatus{State: a2a.TaskStateCanceled},
		}
		writeJSONRPCResult(w, req.ID, task)
	}))
	defer agent.Close()

	srv := NewServer()

	input := CancelTaskInput{
		Agent:  agent.URL,
		TaskID: "task-url",
	}

	result, _, err := srv.handleCancelTask(context.Background(), nil, input)
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
	if textContent.Text != "Task task-url has been canceled" {
		t.Errorf("expected %q, got %q", "Task task-url has been canceled", textContent.Text)
	}
}

func TestHandleCancelTask_CannotBeCanceled(t *testing.T) {
	// Test TaskNotCancelableError with different message text but same error code.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		writeJSONRPCError(w, req.ID, -32002, "task cannot be canceled")
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("nocancelvar-agent", agent.URL)

	input := CancelTaskInput{
		Agent:  "nocancelvar-agent",
		TaskID: "task-789",
	}

	result, _, err := srv.handleCancelTask(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for task cannot be canceled")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	// The SDK maps error code -32002 to ErrTaskNotCancelable regardless of the server's message text.
	if textContent.Text != "task is not cancelable" {
		t.Errorf("expected %q, got %q", "task is not cancelable", textContent.Text)
	}
}
