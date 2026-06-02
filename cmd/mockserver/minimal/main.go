//go:build dev

// Package main provides a mock A2A server for testing the a2a-gateway-mcp client.
// It implements the minimum A2A protocol surface: agent card discovery, message
// handling (echo), task retrieval, task cancellation, and an agent directory endpoint.
// All operations use JSON-RPC 2.0 envelopes per A2A spec §9.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/nisimpson/a2a-gateway-mcp/directory"
)

// jsonrpcRequest represents an incoming JSON-RPC 2.0 request envelope.
type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      any             `json:"id"`
}

// jsonrpcResponse represents an outgoing JSON-RPC 2.0 response envelope.
type jsonrpcResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   any    `json:"error,omitempty"`
}

func main() {
	var (
		port int
		name string
	)

	flag.IntVar(&port, "port", 8080, "port to listen on")
	flag.StringVar(&name, "name", "mock-agent", "agent name for the agent card")
	flag.Parse()

	srv := newMockServer(name, port)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	go func() {
		addr := fmt.Sprintf(":%d", port)
		log.Printf("mock a2a server %q listening on %s", name, addr)
		log.Printf("  agent card: http://localhost:%d/.well-known/agent.json", port)
		log.Printf("  directory:  http://localhost:%d/agents", port)
		if err := srv.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.httpServer.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown error: %v", err)
	}
}

// mockServer is a minimal A2A-compliant server that echoes messages back
// and serves an agent directory. Uses JSON-RPC 2.0 envelopes per §9.
type mockServer struct {
	name       string
	port       int
	httpServer *http.Server
	directory  *directory.Directory

	mu    sync.RWMutex
	tasks map[a2a.TaskID]*a2a.Task
}

func newMockServer(name string, port int) *mockServer {
	dir := directory.New()

	s := &mockServer{
		name:      name,
		port:      port,
		directory: dir,
		tasks:     make(map[a2a.TaskID]*a2a.Task),
	}

	// Register this server's own agent card in the directory.
	card := s.agentCard()
	if err := dir.Register(context.Background(), card); err != nil {
		log.Fatalf("failed to register agent card: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/agent.json", s.handleAgentCard)
	mux.Handle("GET /agents", dir)
	mux.HandleFunc("POST /", s.handlePost)

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	return s
}

// agentCard returns the A2A agent card for this mock server.
func (s *mockServer) agentCard() a2a.AgentCard {
	return a2a.AgentCard{
		Name:        s.name,
		Description: "A mock A2A agent for testing purposes",
		Version:     "1.0.0",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(
				fmt.Sprintf("http://localhost:%d/", s.port),
				a2a.TransportProtocolJSONRPC,
			),
		},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Capabilities:       a2a.AgentCapabilities{},
		Skills: []a2a.AgentSkill{
			{
				ID:          "echo",
				Name:        "Echo",
				Description: "Echoes back the user's message",
				Tags:        []string{"echo", "test"},
				Examples:    []string{"Hello, agent!"},
			},
		},
	}
}

// handleAgentCard serves the agent card at /.well-known/agent.json.
func (s *mockServer) handleAgentCard(w http.ResponseWriter, _ *http.Request) {
	card := s.agentCard()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(card)
}

// handlePost parses incoming JSON-RPC 2.0 requests and routes by method name.
func (s *mockServer) handlePost(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}

	var req jsonrpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSONRPCError(w, nil, -32700, "parse error")
		return
	}

	if req.JSONRPC != "2.0" {
		s.writeJSONRPCError(w, req.ID, -32600, "invalid request: missing jsonrpc 2.0")
		return
	}

	switch req.Method {
	case "SendMessage":
		s.handleSendMessage(w, &req)
	case "GetTask":
		s.handleGetTask(w, &req)
	case "CancelTask":
		s.handleCancelTask(w, &req)
	default:
		s.writeJSONRPCError(w, req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

// handleSendMessage processes a SendMessage JSON-RPC request and returns a completed Task
// that echoes the user's message.
func (s *mockServer) handleSendMessage(w http.ResponseWriter, req *jsonrpcRequest) {
	var sendReq a2a.SendMessageRequest
	if err := json.Unmarshal(req.Params, &sendReq); err != nil {
		s.writeJSONRPCError(w, req.ID, -32602, fmt.Sprintf("invalid params: %v", err))
		return
	}

	// Extract the user's text from the message parts.
	userText := ""
	if sendReq.Message != nil {
		for _, part := range sendReq.Message.Parts {
			if part != nil {
				if t := part.Text(); t != "" {
					userText = t
					break
				}
			}
		}
	}

	// Determine context ID: use the one from the message or generate a new one.
	contextID := ""
	if sendReq.Message != nil {
		contextID = sendReq.Message.ContextID
	}
	if contextID == "" {
		contextID = a2a.NewContextID()
	}

	// Build a completed task with an echo response.
	now := time.Now()
	task := &a2a.Task{
		ID:        a2a.NewTaskID(),
		ContextID: contextID,
		Status: a2a.TaskStatus{
			State:     a2a.TaskStateCompleted,
			Timestamp: &now,
		},
		Artifacts: []*a2a.Artifact{
			{
				ID:    a2a.NewArtifactID(),
				Name:  "response",
				Parts: a2a.ContentParts{a2a.NewTextPart(fmt.Sprintf("echo: %s", userText))},
			},
		},
	}

	// Store the task for later retrieval.
	s.mu.Lock()
	s.tasks[task.ID] = task
	s.mu.Unlock()

	log.Printf("handled message: %q -> task %s", userText, task.ID)

	// Wrap in StreamResponse format that the SDK expects for SendMessage.
	result := map[string]any{
		"kind": "task",
		"task": task,
	}

	s.writeJSONRPCResult(w, req.ID, result)
}

// handleGetTask processes a GetTask JSON-RPC request and returns the stored task.
func (s *mockServer) handleGetTask(w http.ResponseWriter, req *jsonrpcRequest) {
	var getReq a2a.GetTaskRequest
	if err := json.Unmarshal(req.Params, &getReq); err != nil {
		s.writeJSONRPCError(w, req.ID, -32602, fmt.Sprintf("invalid params: %v", err))
		return
	}

	s.mu.RLock()
	task, ok := s.tasks[getReq.ID]
	s.mu.RUnlock()

	if !ok {
		s.writeJSONRPCError(w, req.ID, -32001, "task not found")
		return
	}

	// GetTask returns the task directly as the result (no StreamResponse wrapper).
	s.writeJSONRPCResult(w, req.ID, task)
}

// handleCancelTask processes a CancelTask JSON-RPC request.
func (s *mockServer) handleCancelTask(w http.ResponseWriter, req *jsonrpcRequest) {
	var cancelReq a2a.CancelTaskRequest
	if err := json.Unmarshal(req.Params, &cancelReq); err != nil {
		s.writeJSONRPCError(w, req.ID, -32602, fmt.Sprintf("invalid params: %v", err))
		return
	}

	s.mu.Lock()
	task, ok := s.tasks[cancelReq.ID]
	if !ok {
		s.mu.Unlock()
		s.writeJSONRPCError(w, req.ID, -32001, "task not found")
		return
	}

	// If the task is already in a terminal state, it's not cancelable.
	if task.Status.State == a2a.TaskStateCompleted || task.Status.State == a2a.TaskStateFailed {
		s.mu.Unlock()
		s.writeJSONRPCError(w, req.ID, -32002, "task is not cancelable")
		return
	}

	// Cancel the task.
	now := time.Now()
	task.Status = a2a.TaskStatus{
		State:     a2a.TaskStateCanceled,
		Timestamp: &now,
	}
	s.mu.Unlock()

	// CancelTask returns the task directly as the result.
	s.writeJSONRPCResult(w, req.ID, task)
}

// writeJSONRPCResult writes a successful JSON-RPC response.
func (s *mockServer) writeJSONRPCResult(w http.ResponseWriter, id any, result any) {
	resp := jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// writeJSONRPCError writes a JSON-RPC error response.
func (s *mockServer) writeJSONRPCError(w http.ResponseWriter, id any, code int, message string) {
	resp := jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: map[string]any{
			"code":    code,
			"message": message,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
