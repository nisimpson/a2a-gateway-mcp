package gateway

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// jsonrpcTestRequest represents an incoming JSON-RPC 2.0 request for testing.
type jsonrpcTestRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      string          `json:"id"`
}

// readJSONRPCRequest parses a JSON-RPC request from an HTTP request body.
func readJSONRPCRequest(r *http.Request) (*jsonrpcTestRequest, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	var req jsonrpcTestRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

// jsonrpcTaskResult wraps a task in the StreamResponse format expected by
// the SDK's SendMessage method (which expects {"kind": "task", "task": {...}}).
func jsonrpcTaskResult(task *a2a.Task) map[string]any {
	return map[string]any{"kind": "task", "task": task}
}

// jsonrpcMessageResult wraps a message in the StreamResponse format expected by
// the SDK's SendMessage method (which expects {"kind": "message", "message": {...}}).
func jsonrpcMessageResult(msg *a2a.Message) map[string]any {
	return map[string]any{"kind": "message", "message": msg}
}

// writeJSONRPCResult writes a successful JSON-RPC 2.0 response.
func writeJSONRPCResult(w http.ResponseWriter, reqID string, result any) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      reqID,
		"result":  result,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// writeJSONRPCError writes a JSON-RPC 2.0 error response.
func writeJSONRPCError(w http.ResponseWriter, reqID string, code int, message string) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      reqID,
		"error":   map[string]any{"code": code, "message": message},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// newTestServerWithAgent creates a Server with a registered agent pointing to
// the given URL. It sets an AgentCard with JSON-RPC protocol binding so that
// the clientResolver creates a JSON-RPC transport client.
func newTestServerWithAgent(alias, agentURL string) *Server {
	srv := NewServer()
	srv.registry.Connect(alias, agentURL, nil)
	srv.registry.SetCard(alias, &a2a.AgentCard{
		Name: alias,
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(agentURL, a2a.TransportProtocolJSONRPC),
		},
	})
	return srv
}
