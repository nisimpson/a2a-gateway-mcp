package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// =============================================================================
// Task 8.4: Integration tests for streaming in handleSendMessage
// Requirements: STRM-1.1, STRM-4.1, STRM-4.2, STRM-4.3, STRM-6.1, STRM-7.3
// =============================================================================

// newTestServerWithStreamingAgent creates a Server with a registered agent that
// has Streaming=true in its card. The card also sets up a JSON-RPC interface.
func newTestServerWithStreamingAgent(alias, agentURL string) *Server {
	srv := NewServer()
	srv.registry.Connect(alias, agentURL, nil, "")
	srv.registry.SetCard(alias, &a2a.AgentCard{
		Name:         alias,
		Capabilities: a2a.AgentCapabilities{Streaming: true},
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(agentURL, a2a.TransportProtocolJSONRPC),
		},
	})
	return srv
}

// writeSSEEvent writes a single SSE data line followed by a blank line.
func writeSSEEvent(w http.ResponseWriter, data []byte) {
	fmt.Fprintf(w, "data: %s\n\n", data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// writeStreamingTaskEvent writes a properly formatted SSE event containing
// a JSON-RPC response with a StreamResponse wrapping a Task.
func writeStreamingTaskEvent(w http.ResponseWriter, task *a2a.Task) {
	// StreamResponse format: {"task": {...}}
	streamResp := map[string]any{"task": task}
	resultJSON, _ := json.Marshal(streamResp)
	// JSON-RPC response wrapping the stream response
	rpcResp := map[string]any{
		"jsonrpc": "2.0",
		"id":      "1",
		"result":  json.RawMessage(resultJSON),
	}
	data, _ := json.Marshal(rpcResp)
	writeSSEEvent(w, data)
}

func TestHandleSendMessage_RoutesToStreaming_WhenCapabilityPresent(t *testing.T) {
	// Agent that serves streaming SSE response with a completed task event
	var receivedMethod string
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		receivedMethod = req.Method

		if req.Method == "SendStreamingMessage" {
			// Return a Task event via SSE
			task := &a2a.Task{
				ID:        "stream-task-1",
				ContextID: "stream-ctx-1",
				Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
				Artifacts: []*a2a.Artifact{
					{Parts: a2a.ContentParts{a2a.NewTextPart("streamed response")}},
				},
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			writeStreamingTaskEvent(w, task)
			return
		}

		// Fallback: should not reach here if streaming is used
		writeJSONRPCError(w, req.ID, -32601, "should have used streaming")
	}))
	defer agent.Close()

	srv := newTestServerWithStreamingAgent("stream-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "stream-agent",
		Message: "hello streaming",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Verify the streaming method was called
	if receivedMethod != "SendStreamingMessage" {
		t.Errorf("expected method %q, got %q", "SendStreamingMessage", receivedMethod)
	}
}

func TestHandleSendMessage_UsesPolling_WhenNoStreamingCapability(t *testing.T) {
	// Agent without streaming capability
	var receivedMethod string
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		receivedMethod = req.Method

		task := &a2a.Task{
			ID:        "poll-task-1",
			ContextID: "poll-ctx-1",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("polled response")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	// Use non-streaming agent (Streaming: false)
	srv := newTestServerWithAgent("poll-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "poll-agent",
		Message: "hello polling",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Verify the non-streaming method was called
	if receivedMethod != "SendMessage" {
		t.Errorf("expected method %q, got %q", "SendMessage", receivedMethod)
	}
}

func TestHandleSendMessage_Streaming_ContextIDFromInput(t *testing.T) {
	// Verify that explicit context_id is passed through to the streaming request
	var receivedContextID string
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)

		if req.Method == "SendStreamingMessage" {
			var params a2a.SendMessageRequest
			_ = json.Unmarshal(req.Params, &params)
			receivedContextID = params.Message.ContextID

			task := &a2a.Task{
				ID:        "ctx-task-1",
				ContextID: "ctx-response-1",
				Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
				Artifacts: []*a2a.Artifact{
					{Parts: a2a.ContentParts{a2a.NewTextPart("ok")}},
				},
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			writeStreamingTaskEvent(w, task)
			return
		}
		writeJSONRPCError(w, req.ID, -32601, "unexpected method")
	}))
	defer agent.Close()

	srv := newTestServerWithStreamingAgent("ctx-stream-agent", agent.URL)

	input := SendMessageInput{
		Agent:     "ctx-stream-agent",
		Message:   "hello",
		ContextID: "ctx-explicit-42",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	if receivedContextID != "ctx-explicit-42" {
		t.Errorf("expected context_id %q in streaming request, got %q", "ctx-explicit-42", receivedContextID)
	}
}

func TestHandleSendMessage_Streaming_TaskIDFromInput(t *testing.T) {
	// Verify that task_id from input flows through to the streaming request
	var receivedTaskID string
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)

		if req.Method == "SendStreamingMessage" {
			var params a2a.SendMessageRequest
			_ = json.Unmarshal(req.Params, &params)
			receivedTaskID = string(params.Message.TaskID)

			task := &a2a.Task{
				ID:        "task-from-input",
				ContextID: "ctx-1",
				Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
				Artifacts: []*a2a.Artifact{
					{Parts: a2a.ContentParts{a2a.NewTextPart("ok")}},
				},
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			writeStreamingTaskEvent(w, task)
			return
		}
		writeJSONRPCError(w, req.ID, -32601, "unexpected method")
	}))
	defer agent.Close()

	srv := newTestServerWithStreamingAgent("taskid-stream-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "taskid-stream-agent",
		Message: "follow up",
		TaskID:  "task-ref-99",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	if receivedTaskID != "task-ref-99" {
		t.Errorf("expected task_id %q in streaming request, got %q", "task-ref-99", receivedTaskID)
	}
}

func TestHandleSendMessage_Streaming_ContextIDStoredFromResponse(t *testing.T) {
	// Verify that context_id from stream response is stored in context store
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)

		if req.Method == "SendStreamingMessage" {
			task := &a2a.Task{
				ID:        "store-task-1",
				ContextID: "ctx-from-stream",
				Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
				Artifacts: []*a2a.Artifact{
					{Parts: a2a.ContentParts{a2a.NewTextPart("done")}},
				},
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			writeStreamingTaskEvent(w, task)
			return
		}
		writeJSONRPCError(w, req.ID, -32601, "unexpected method")
	}))
	defer agent.Close()

	srv := newTestServerWithStreamingAgent("store-ctx-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "store-ctx-agent",
		Message: "hello",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Verify context store was updated with stream response context_id
	stored := srv.contextStore.Get("store-ctx-agent")
	if stored != "ctx-from-stream" {
		t.Errorf("expected context store to have %q, got %q", "ctx-from-stream", stored)
	}
}

func TestHandleSendMessage_Streaming_NoFallbackToPolling(t *testing.T) {
	// Verify that once streaming is selected, there's no fallback to polling.
	// The agent's streaming endpoint returns an error, which should be surfaced
	// directly without falling back to polling.
	var methodsCalled []string
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		methodsCalled = append(methodsCalled, req.Method)

		if req.Method == "SendStreamingMessage" {
			// Return an SSE stream with an error (malformed response that the SDK
			// won't be able to parse into valid events)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			// Write invalid data that the SDK will treat as an error or empty stream
			fmt.Fprintf(w, "data: {\"invalid\": true}\n\n")
			return
		}

		// This should NOT be called — no fallback
		task := &a2a.Task{
			ID:     "fallback-task",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("from polling")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := newTestServerWithStreamingAgent("nofallback-agent", agent.URL)

	input := SendMessageInput{
		Agent:   "nofallback-agent",
		Message: "hello",
	}

	result, _, err := srv.handleSendMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The key assertion is that "SendMessage" was never called (no fallback).
	for _, method := range methodsCalled {
		if method == "SendMessage" {
			t.Error("expected no fallback to polling (SendMessage should not be called)")
		}
	}

	// Only "SendStreamingMessage" should have been called
	if len(methodsCalled) == 0 {
		t.Error("expected at least one method call")
	}
	if methodsCalled[0] != "SendStreamingMessage" {
		t.Errorf("expected first method to be %q, got %q", "SendStreamingMessage", methodsCalled[0])
	}

	// If the result is an error, that's expected — streaming error without fallback
	if result.IsError {
		// This is correct behavior: streaming failed and no fallback occurred
		return
	}
}
