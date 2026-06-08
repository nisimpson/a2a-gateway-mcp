package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// =============================================================================
// Task 8.5: Integration tests for streaming in broadcast
// Requirements: STRM-5.1, STRM-5.2, STRM-5.3, STRM-5.4
// =============================================================================

func TestBroadcast_StreamingForCapableAgent_PollingForNon(t *testing.T) {
	// Set up a streaming agent that serves SSE
	var streamMethodCalled bool
	streamingAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		if req.Method == "SendStreamingMessage" {
			streamMethodCalled = true
			task := &a2a.Task{
				ID:     "stream-task",
				Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
				Artifacts: []*a2a.Artifact{
					{Parts: a2a.ContentParts{a2a.NewTextPart("streamed broadcast")}},
				},
			}
			// StreamResponse format for SSE
			streamResp := map[string]any{"task": task}
			resultJSON, _ := json.Marshal(streamResp)
			rpcResp := map[string]any{
				"jsonrpc": "2.0",
				"id":      "1",
				"result":  json.RawMessage(resultJSON),
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			data, _ := json.Marshal(rpcResp)
			fmt.Fprintf(w, "data: %s\n\n", data)
			return
		}
		writeJSONRPCError(w, req.ID, -32601, "should have used streaming")
	}))
	defer streamingAgent.Close()

	// Set up a non-streaming agent that serves JSON-RPC
	var pollMethodCalled bool
	pollingAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		if req.Method == "SendMessage" {
			pollMethodCalled = true
			task := &a2a.Task{
				ID:     "poll-task",
				Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
				Artifacts: []*a2a.Artifact{
					{Parts: a2a.ContentParts{a2a.NewTextPart("polled broadcast")}},
				},
			}
			writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
			return
		}
		writeJSONRPCError(w, req.ID, -32601, "unexpected method: "+req.Method)
	}))
	defer pollingAgent.Close()

	srv := NewServer()

	// Register streaming agent with Streaming=true
	srv.registry.Connect("streaming-bcast", streamingAgent.URL, nil, "")
	srv.registry.SetCard("streaming-bcast", &a2a.AgentCard{
		Name:         "streaming-bcast",
		Capabilities: a2a.AgentCapabilities{Streaming: true},
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(streamingAgent.URL, a2a.TransportProtocolJSONRPC),
		},
	})

	// Register non-streaming agent with Streaming=false (default)
	srv.registry.Connect("polling-bcast", pollingAgent.URL, nil, "")
	srv.registry.SetCard("polling-bcast", &a2a.AgentCard{
		Name:         "polling-bcast",
		Capabilities: a2a.AgentCapabilities{Streaming: false},
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(pollingAgent.URL, a2a.TransportProtocolJSONRPC),
		},
	})

	input := BroadcastMessageInput{
		Aliases: []string{"streaming-bcast", "polling-bcast"},
		Message: "broadcast to mixed agents",
	}

	result, _, err := srv.handleBroadcastMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Verify streaming was used for the streaming agent
	if !streamMethodCalled {
		t.Error("expected streaming agent to receive message/stream call")
	}

	// Verify polling was used for the non-streaming agent
	if !pollMethodCalled {
		t.Error("expected non-streaming agent to receive message/send call")
	}

	// Parse results
	textContent := result.Content[0].(*mcp.TextContent)
	var results map[string]*broadcastResult
	if err := json.Unmarshal([]byte(textContent.Text), &results); err != nil {
		t.Fatalf("failed to parse results: %v", err)
	}

	// Both should succeed
	if results["streaming-bcast"].Status != "success" {
		t.Errorf("expected success for streaming agent, got %s", results["streaming-bcast"].Status)
	}
	if results["polling-bcast"].Status != "success" {
		t.Errorf("expected success for polling agent, got %s", results["polling-bcast"].Status)
	}
}

func TestBroadcast_PerAgentTimeout_AppliedAsStreamTimeout(t *testing.T) {
	// Agent that delays response longer than the timeout
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)

		if req.Method == "SendStreamingMessage" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			// Flush headers
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			// Delay longer than the timeout before sending the terminal event
			time.Sleep(3 * time.Second)
			task := &a2a.Task{
				ID:     "slow-task",
				Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
				Artifacts: []*a2a.Artifact{
					{Parts: a2a.ContentParts{a2a.NewTextPart("should not see")}},
				},
			}
			streamResp := map[string]any{"task": task}
			resultJSON, _ := json.Marshal(streamResp)
			rpcResp := map[string]any{
				"jsonrpc": "2.0",
				"id":      "1",
				"result":  json.RawMessage(resultJSON),
			}
			data, _ := json.Marshal(rpcResp)
			fmt.Fprintf(w, "data: %s\n\n", data)
			return
		}
		writeJSONRPCError(w, req.ID, -32601, "unexpected")
	}))
	defer agent.Close()

	srv := NewServer()
	srv.registry.Connect("slow-stream", agent.URL, nil, "")
	srv.registry.SetCard("slow-stream", &a2a.AgentCard{
		Name:         "slow-stream",
		Capabilities: a2a.AgentCapabilities{Streaming: true},
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(agent.URL, a2a.TransportProtocolJSONRPC),
		},
	})

	timeout := 1 // 1 second timeout
	input := BroadcastMessageInput{
		Aliases:        []string{"slow-stream"},
		Message:        "timeout test",
		TimeoutSeconds: &timeout,
	}

	result, _, err := srv.handleBroadcastMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected non-error result at top level")
	}

	textContent := result.Content[0].(*mcp.TextContent)
	var results map[string]*broadcastResult
	if err := json.Unmarshal([]byte(textContent.Text), &results); err != nil {
		t.Fatalf("failed to parse results: %v", err)
	}

	r, exists := results["slow-stream"]
	if !exists {
		t.Fatal("missing result for slow-stream")
	}
	if r.Status != "error" {
		t.Errorf("expected error due to timeout, got status %q", r.Status)
	}
}

func TestBroadcast_ResultShapeIdentical_StreamingAndNonStreaming(t *testing.T) {
	// Both agents return the same logical response, one via streaming, one via polling.
	// Verify the broadcastResult shape is identical.
	taskResponse := &a2a.Task{
		ID:     "same-task",
		Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
		Artifacts: []*a2a.Artifact{
			{Parts: a2a.ContentParts{a2a.NewTextPart("identical response")}},
		},
	}

	// Streaming agent
	streamingAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		if req.Method == "SendStreamingMessage" {
			streamResp := map[string]any{"task": taskResponse}
			resultJSON, _ := json.Marshal(streamResp)
			rpcResp := map[string]any{
				"jsonrpc": "2.0",
				"id":      "1",
				"result":  json.RawMessage(resultJSON),
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			data, _ := json.Marshal(rpcResp)
			fmt.Fprintf(w, "data: %s\n\n", data)
			return
		}
		writeJSONRPCError(w, req.ID, -32601, "unexpected")
	}))
	defer streamingAgent.Close()

	// Non-streaming agent
	pollingAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(taskResponse))
	}))
	defer pollingAgent.Close()

	srv := NewServer()
	srv.registry.Connect("stream-shape", streamingAgent.URL, nil, "")
	srv.registry.SetCard("stream-shape", &a2a.AgentCard{
		Name:         "stream-shape",
		Capabilities: a2a.AgentCapabilities{Streaming: true},
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(streamingAgent.URL, a2a.TransportProtocolJSONRPC),
		},
	})
	srv.registry.Connect("poll-shape", pollingAgent.URL, nil, "")
	srv.registry.SetCard("poll-shape", &a2a.AgentCard{
		Name:         "poll-shape",
		Capabilities: a2a.AgentCapabilities{Streaming: false},
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(pollingAgent.URL, a2a.TransportProtocolJSONRPC),
		},
	})

	input := BroadcastMessageInput{
		Aliases: []string{"stream-shape", "poll-shape"},
		Message: "shape test",
	}

	result, _, err := srv.handleBroadcastMessage(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	textContent := result.Content[0].(*mcp.TextContent)
	var results map[string]*broadcastResult
	if err := json.Unmarshal([]byte(textContent.Text), &results); err != nil {
		t.Fatalf("failed to parse results: %v", err)
	}

	streamResult := results["stream-shape"]
	pollResult := results["poll-shape"]

	if streamResult == nil || pollResult == nil {
		t.Fatal("expected results for both agents")
	}

	// The broadcastResult shape should be identical
	if streamResult.Status != pollResult.Status {
		t.Errorf("Status mismatch: streaming=%q, polling=%q", streamResult.Status, pollResult.Status)
	}
	if streamResult.Response != pollResult.Response {
		t.Errorf("Response mismatch: streaming=%q, polling=%q", streamResult.Response, pollResult.Response)
	}
	if streamResult.Error != pollResult.Error {
		t.Errorf("Error mismatch: streaming=%q, polling=%q", streamResult.Error, pollResult.Error)
	}
}
