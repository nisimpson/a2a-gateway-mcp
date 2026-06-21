package tool

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nisimpson/a2a-gateway-mcp/internal/history"
)

// --- Mock AgentRegistry ---

type mockRegistry struct {
	LookupFn            func(alias string) *AgentEntry
	ListFn              func() []*AgentEntry
	ConnectFn           func(alias, url string, headers map[string]string, pingEndpoint string) bool
	DisconnectFn        func(alias string) *AgentEntry
	SetCardFn           func(alias string, card *a2a.AgentCard) bool
	ResolveAgentFn      func(identifier string) (*ResolveResult, error)
	SupportsStreamingFn func(resolved *ResolveResult) bool
}

func (m *mockRegistry) Lookup(alias string) *AgentEntry {
	if m.LookupFn != nil {
		return m.LookupFn(alias)
	}
	return nil
}
func (m *mockRegistry) List() []*AgentEntry {
	if m.ListFn != nil {
		return m.ListFn()
	}
	return nil
}
func (m *mockRegistry) Connect(alias, url string, headers map[string]string, pingEndpoint string) bool {
	if m.ConnectFn != nil {
		return m.ConnectFn(alias, url, headers, pingEndpoint)
	}
	return false
}
func (m *mockRegistry) Disconnect(alias string) *AgentEntry {
	if m.DisconnectFn != nil {
		return m.DisconnectFn(alias)
	}
	return nil
}
func (m *mockRegistry) SetCard(alias string, card *a2a.AgentCard) bool {
	if m.SetCardFn != nil {
		return m.SetCardFn(alias, card)
	}
	return false
}
func (m *mockRegistry) ResolveAgent(identifier string) (*ResolveResult, error) {
	if m.ResolveAgentFn != nil {
		return m.ResolveAgentFn(identifier)
	}
	return &ResolveResult{URL: "http://example.com", IsAlias: true, Alias: identifier}, nil
}
func (m *mockRegistry) SupportsStreaming(resolved *ResolveResult) bool {
	if m.SupportsStreamingFn != nil {
		return m.SupportsStreamingFn(resolved)
	}
	return false
}

// --- Mock HealthTracker ---

type mockHealthTracker struct {
	RecordFailureFn func(alias string)
	RecordSuccessFn func(alias string)
	IsEnabledFn     func() bool
	IsHealthyFn     func(alias string) bool
	GetStatusFn     func(alias string) string
	ResetFn         func(alias string)
	GetFailuresFn   func(alias string) (int, bool)
}

func (m *mockHealthTracker) RecordFailure(alias string) {
	if m.RecordFailureFn != nil {
		m.RecordFailureFn(alias)
	}
}
func (m *mockHealthTracker) RecordSuccess(alias string) {
	if m.RecordSuccessFn != nil {
		m.RecordSuccessFn(alias)
	}
}
func (m *mockHealthTracker) IsEnabled() bool {
	if m.IsEnabledFn != nil {
		return m.IsEnabledFn()
	}
	return false
}
func (m *mockHealthTracker) IsHealthy(alias string) bool {
	if m.IsHealthyFn != nil {
		return m.IsHealthyFn(alias)
	}
	return true
}
func (m *mockHealthTracker) GetStatus(alias string) string {
	if m.GetStatusFn != nil {
		return m.GetStatusFn(alias)
	}
	return "unknown"
}
func (m *mockHealthTracker) Reset(alias string) {
	if m.ResetFn != nil {
		m.ResetFn(alias)
	}
}
func (m *mockHealthTracker) GetFailures(alias string) (int, bool) {
	if m.GetFailuresFn != nil {
		return m.GetFailuresFn(alias)
	}
	return 0, false
}

// --- Mock RateLimiter ---

type mockRateLimiter struct {
	AllowFn          func(alias string) bool
	CheckRateLimitFn func(alias string) *mcp.CallToolResult
	SetFn            func(alias string, rps float64, burst int)
	RemoveFn         func(alias string)
	GetFn            func(alias string) (float64, int, bool)
}

func (m *mockRateLimiter) Allow(alias string) bool {
	if m.AllowFn != nil {
		return m.AllowFn(alias)
	}
	return true
}
func (m *mockRateLimiter) CheckRateLimit(alias string) *mcp.CallToolResult {
	if m.CheckRateLimitFn != nil {
		return m.CheckRateLimitFn(alias)
	}
	return nil
}
func (m *mockRateLimiter) Set(alias string, rps float64, burst int) {
	if m.SetFn != nil {
		m.SetFn(alias, rps, burst)
	}
}
func (m *mockRateLimiter) Remove(alias string) {
	if m.RemoveFn != nil {
		m.RemoveFn(alias)
	}
}
func (m *mockRateLimiter) Get(alias string) (float64, int, bool) {
	if m.GetFn != nil {
		return m.GetFn(alias)
	}
	return 0, 0, false
}

// --- Mock ContextStore ---

type mockContextStore struct {
	store map[string]string
}

func newMockContextStore() *mockContextStore {
	return &mockContextStore{store: make(map[string]string)}
}
func (m *mockContextStore) Get(alias string) string     { return m.store[alias] }
func (m *mockContextStore) Set(alias, contextID string) { m.store[alias] = contextID }
func (m *mockContextStore) Delete(alias string)         { delete(m.store, alias) }

// --- Mock CallerCardInjector ---

type mockCallerCardInjector struct{}

func (m *mockCallerCardInjector) InjectCallerCard(metadata map[string]any) map[string]any {
	return metadata
}

// --- Mock HistoryRecorder ---

type mockHistoryRecorder struct {
	records []history.RecordInput
}

func (m *mockHistoryRecorder) Record(_ context.Context, input history.RecordInput) {
	m.records = append(m.records, input)
}

// --- Mock A2AClientResolver ---

type mockClientResolver struct {
	ResolveFn func(ctx context.Context, resolved *ResolveResult) (*a2aclient.Client, error)
	EvictFn   func(url string)
}

func (m *mockClientResolver) Resolve(ctx context.Context, resolved *ResolveResult) (*a2aclient.Client, error) {
	if m.ResolveFn != nil {
		return m.ResolveFn(ctx, resolved)
	}
	return nil, nil
}
func (m *mockClientResolver) Evict(url string) {
	if m.EvictFn != nil {
		m.EvictFn(url)
	}
}

// --- JSON-RPC test helpers ---

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      string          `json:"id"`
}

func readJSONRPCRequest(r *http.Request) (*jsonrpcRequest, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	var req jsonrpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

func writeJSONRPCResult(w http.ResponseWriter, reqID string, result any) {
	resp := map[string]any{"jsonrpc": "2.0", "id": reqID, "result": result}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func jsonrpcTaskResult(task *a2a.Task) map[string]any {
	return map[string]any{"kind": "task", "task": task}
}

func jsonrpcMessageResult(msg *a2a.Message) map[string]any {
	return map[string]any{"kind": "message", "message": msg}
}

// newTestClient creates a real a2aclient.Client pointing at the given URL
// using JSON-RPC transport (for httptest servers).
func newTestClient(ctx context.Context, url string) (*a2aclient.Client, error) {
	endpoints := []*a2a.AgentInterface{
		a2a.NewAgentInterface(url, a2a.TransportProtocolJSONRPC),
	}
	return a2aclient.NewFromEndpoints(ctx, endpoints)
}
