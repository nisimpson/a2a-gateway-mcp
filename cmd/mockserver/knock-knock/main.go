//go:build dev

// Package main provides a knock-knock joke A2A agent for testing multi-turn
// input-required flows through the gateway. The conversation proceeds as:
//
//	caller: "tell a joke"       -> agent: "Knock knock" (input-required)
//	caller: "who's there?"      -> agent: "<setup>" (input-required)
//	caller: "<setup> who?"      -> agent: "<punchline>!" (completed)
//
// This exercises the gateway's handling of input-required as a turn-terminal
// state and context_id-based conversation resumption.
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
	"strings"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
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
	var port int

	flag.IntVar(&port, "port", 9000, "port to listen on")
	flag.Parse()

	srv := newKnockKnockServer(port)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	go func() {
		addr := fmt.Sprintf(":%d", port)
		log.Printf("knock-knock agent listening on %s", addr)
		log.Printf("  agent card: http://localhost:%d/.well-known/agent-card.json", port)
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

// jokeStep tracks the current position in a knock-knock joke conversation.
type jokeStep int

const (
	stepStart    jokeStep = iota // Awaiting initial "tell a joke"
	stepKnock                    // Agent said "Knock knock", waiting for "who's there?"
	stepSetup                    // Agent said the setup, waiting for "<setup> who?"
	stepComplete                 // Joke delivered
)

// conversation tracks the state of an in-progress knock-knock joke.
type conversation struct {
	step      jokeStep
	setup     string // e.g. "Atch"
	punchline string // e.g. "Bless you!"
}

// jokes is a rotating set of knock-knock jokes.
var jokes = []struct {
	setup     string
	punchline string
}{
	{"Atch", "Bless you!"},
	{"Nobel", "Nobel, that's why I knocked!"},
	{"Cow goes", "No, a cow goes moo!"},
	{"Interrupting cow", "Mooooo!"},
	{"Boo", "Don't cry, it's just a joke!"},
}

type knockKnockServer struct {
	port       int
	httpServer *http.Server

	mu      sync.Mutex
	convos  map[string]*conversation // key: context_id
	jokeIdx int                      // rotating joke index
}

func newKnockKnockServer(port int) *knockKnockServer {
	s := &knockKnockServer{
		port:   port,
		convos: make(map[string]*conversation),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/agent-card.json", s.handleAgentCard)
	mux.HandleFunc("POST /", s.handlePost)

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	return s
}

func (s *knockKnockServer) agentCard() a2a.AgentCard {
	return a2a.AgentCard{
		Name:        "knock-knock",
		Description: "A knock-knock joke agent that tests multi-turn input-required flows",
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
				ID:          "knock-knock",
				Name:        "Knock-Knock Joke",
				Description: "Tells a knock-knock joke through a multi-turn conversation",
				Tags:        []string{"joke", "multi-turn", "input-required"},
				Examples:    []string{"Tell me a joke", "Knock knock"},
			},
		},
	}
}

func (s *knockKnockServer) handleAgentCard(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.agentCard())
}

// handlePost parses incoming JSON-RPC 2.0 requests and routes by method name.
func (s *knockKnockServer) handlePost(w http.ResponseWriter, r *http.Request) {
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
	default:
		s.writeJSONRPCError(w, req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

// handleSendMessage processes a SendMessage JSON-RPC request for the multi-turn
// knock-knock joke conversation.
func (s *knockKnockServer) handleSendMessage(w http.ResponseWriter, req *jsonrpcRequest) {
	var sendReq a2a.SendMessageRequest
	if err := json.Unmarshal(req.Params, &sendReq); err != nil {
		s.writeJSONRPCError(w, req.ID, -32602, fmt.Sprintf("invalid params: %v", err))
		return
	}

	// Extract user text.
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
	userText = strings.TrimSpace(strings.ToLower(userText))

	// Determine context_id.
	contextID := ""
	if sendReq.Message != nil {
		contextID = sendReq.Message.ContextID
	}
	if contextID == "" {
		contextID = a2a.NewContextID()
	}

	s.mu.Lock()

	// Look up or create conversation.
	convo, exists := s.convos[contextID]
	if !exists {
		// New conversation: pick a joke and start.
		joke := jokes[s.jokeIdx%len(jokes)]
		s.jokeIdx++
		convo = &conversation{
			step:      stepStart,
			setup:     joke.setup,
			punchline: joke.punchline,
		}
		s.convos[contextID] = convo
	}

	var responseText string
	var state a2a.TaskState

	switch convo.step {
	case stepStart:
		// Caller initiated — respond with "Knock knock"
		responseText = "Knock knock"
		state = a2a.TaskStateInputRequired
		convo.step = stepKnock
		log.Printf("[%s] start -> knock knock", contextID)

	case stepKnock:
		// Expecting "who's there?"
		responseText = convo.setup
		state = a2a.TaskStateInputRequired
		convo.step = stepSetup
		log.Printf("[%s] who's there? -> %s", contextID, convo.setup)

	case stepSetup:
		// Expecting "<setup> who?"
		responseText = fmt.Sprintf("%s %s", convo.setup, convo.punchline)
		state = a2a.TaskStateCompleted
		convo.step = stepComplete
		// Clean up completed conversation.
		delete(s.convos, contextID)
		log.Printf("[%s] punchline -> %s %s", contextID, convo.setup, convo.punchline)

	default:
		// Conversation already done or in unexpected state.
		responseText = "That joke's over! Say 'tell me a joke' to start a new one."
		state = a2a.TaskStateCompleted
		delete(s.convos, contextID)
	}

	s.mu.Unlock()

	now := time.Now()
	task := &a2a.Task{
		ID:        a2a.NewTaskID(),
		ContextID: contextID,
		Status: a2a.TaskStatus{
			State:     state,
			Timestamp: &now,
			Message:   a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(responseText)),
		},
	}

	// For completed tasks, also include the response as an artifact.
	if state == a2a.TaskStateCompleted {
		task.Artifacts = []*a2a.Artifact{
			{
				ID:    a2a.NewArtifactID(),
				Name:  "punchline",
				Parts: a2a.ContentParts{a2a.NewTextPart(responseText)},
			},
		}
	}

	// Wrap in StreamResponse format that the SDK expects for SendMessage.
	result := map[string]any{
		"kind": "task",
		"task": task,
	}

	s.writeJSONRPCResult(w, req.ID, result)

	_ = userText // logged above for context
}

// writeJSONRPCResult writes a successful JSON-RPC response.
func (s *knockKnockServer) writeJSONRPCResult(w http.ResponseWriter, id any, result any) {
	resp := jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// writeJSONRPCError writes a JSON-RPC error response.
func (s *knockKnockServer) writeJSONRPCError(w http.ResponseWriter, id any, code int, message string) {
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
