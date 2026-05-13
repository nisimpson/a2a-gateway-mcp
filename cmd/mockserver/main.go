// Package main provides a mock A2A server for testing the a2a-gateway-mcp client.
// It implements the minimum A2A protocol surface: agent card discovery, message
// handling (echo), task retrieval, and an agent directory endpoint.
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
// and serves an agent directory.
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
				a2a.TransportProtocolHTTPJSON,
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

// handlePost routes POST requests based on the JSON body content.
// It distinguishes between SendMessageRequest and GetTaskRequest.
func (s *mockServer) handlePost(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}

	// Decode into a generic map to determine request type.
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// If the body has a "message" field, it's a SendMessageRequest.
	// If it has an "id" field (without "message"), it's a GetTaskRequest.
	if _, ok := raw["message"]; ok {
		s.handleSendMessage(w, raw)
		return
	}

	if _, ok := raw["id"]; ok {
		s.handleGetTask(w, raw)
		return
	}

	http.Error(w, "unrecognized request format", http.StatusBadRequest)
}

// handleSendMessage processes a SendMessageRequest and returns a completed Task
// that echoes the user's message.
func (s *mockServer) handleSendMessage(w http.ResponseWriter, raw map[string]json.RawMessage) {
	// Re-marshal and decode as SendMessageRequest.
	data, _ := json.Marshal(raw)
	var req a2a.SendMessageRequest
	if err := json.Unmarshal(data, &req); err != nil {
		http.Error(w, fmt.Sprintf("invalid SendMessageRequest: %v", err), http.StatusBadRequest)
		return
	}

	// Extract the user's text from the message parts.
	userText := ""
	if req.Message != nil {
		for _, part := range req.Message.Parts {
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
	if req.Message != nil {
		contextID = req.Message.ContextID
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

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(task)
}

// handleGetTask processes a GetTaskRequest and returns the stored task.
func (s *mockServer) handleGetTask(w http.ResponseWriter, raw map[string]json.RawMessage) {
	data, _ := json.Marshal(raw)
	var req a2a.GetTaskRequest
	if err := json.Unmarshal(data, &req); err != nil {
		http.Error(w, fmt.Sprintf("invalid GetTaskRequest: %v", err), http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	task, ok := s.tasks[req.ID]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, fmt.Sprintf("task %s not found", req.ID), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(task)
}
