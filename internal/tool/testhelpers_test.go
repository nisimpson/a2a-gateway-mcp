package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/nisimpson/a2a-gateway-mcp/history"
	"github.com/nisimpson/a2a-gateway-mcp/registry"
)

// --- Mock AgentRegistry ---

type mockRegistry struct {
	LookupFn            func(alias string) *registry.RegisteredAgent
	ListFn              func() []*registry.RegisteredAgent
	ConnectFn           func(alias, url string, headers map[string]string, pingEndpoint string) bool
	DisconnectFn        func(alias string) *registry.RegisteredAgent
	SetCardFn           func(alias string, card *a2a.AgentCard) bool
	ResolveAgentFn      func(identifier string) (*registry.ResolveResult, error)
	SupportsStreamingFn func(resolved *registry.ResolveResult) bool
}

func (m *mockRegistry) Lookup(alias string) *registry.RegisteredAgent {
	if m.LookupFn != nil {
		return m.LookupFn(alias)
	}
	return nil
}
func (m *mockRegistry) List() []*registry.RegisteredAgent {
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
func (m *mockRegistry) Disconnect(alias string) *registry.RegisteredAgent {
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
func (m *mockRegistry) ResolveAgent(identifier string) (*registry.ResolveResult, error) {
	if m.ResolveAgentFn != nil {
		return m.ResolveAgentFn(identifier)
	}
	return &registry.ResolveResult{URL: "http://example.com", IsAlias: true, Alias: identifier}, nil
}
func (m *mockRegistry) SupportsStreaming(resolved *registry.ResolveResult) bool {
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
	CheckRateLimitFn func(alias string) error
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
func (m *mockRateLimiter) CheckRateLimit(alias string) error {
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
	ResolveFn func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error)
	EvictFn   func(url string)
}

func (m *mockClientResolver) Resolve(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
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

// --- Mock HTTPDoer ---

type mockHTTPDoer struct {
	DoFn func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	if m.DoFn != nil {
		return m.DoFn(req)
	}
	return nil, fmt.Errorf("not implemented")
}

// newTestClient creates a real a2aclient.Client pointing at the given URL
// using JSON-RPC transport (for httptest servers).
func newTestClient(ctx context.Context, url string) (*a2aclient.Client, error) {
	endpoints := []*a2a.AgentInterface{
		a2a.NewAgentInterface(url, a2a.TransportProtocolJSONRPC),
	}
	return a2aclient.NewFromEndpoints(ctx, endpoints)
}

// --- Mock AgentCardFetcher ---

type mockCardFetcher struct {
	FetchAgentCardFn func(ctx context.Context, agentURL string, headers map[string]string) *a2a.AgentCard
}

func (m *mockCardFetcher) FetchAgentCard(ctx context.Context, agentURL string, headers map[string]string) *a2a.AgentCard {
	if m.FetchAgentCardFn != nil {
		return m.FetchAgentCardFn(ctx, agentURL, headers)
	}
	return nil
}

// --- Mock HistoryBackend ---

type mockHistoryBackend struct {
	ListFn   func(ctx context.Context, alias string) ([]history.Entry, error)
	ClearFn  func(ctx context.Context, alias string) error
	DeleteFn func(ctx context.Context, alias string) error
}

func (m *mockHistoryBackend) List(ctx context.Context, alias string) ([]history.Entry, error) {
	if m.ListFn != nil {
		return m.ListFn(ctx, alias)
	}
	return nil, nil
}

func (m *mockHistoryBackend) Clear(ctx context.Context, alias string) error {
	if m.ClearFn != nil {
		return m.ClearFn(ctx, alias)
	}
	return nil
}

func (m *mockHistoryBackend) Delete(ctx context.Context, alias string) error {
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, alias)
	}
	return nil
}

// --- Mock PingStrategy ---

type mockPingStrategy struct {
	PingFn func(ctx context.Context, target PingTarget) PingResult
}

func (m *mockPingStrategy) Ping(ctx context.Context, target PingTarget) PingResult {
	if m.PingFn != nil {
		return m.PingFn(ctx, target)
	}
	return PingResult{}
}

// --- Mock CallerCardStore ---

type mockCallerCardStore struct {
	card        *CallerCard
	metadataKey string
}

func (m *mockCallerCardStore) Set(card *CallerCard, metadataKey string) {
	m.card = card
	m.metadataKey = metadataKey
}

func (m *mockCallerCardStore) Get() *CallerCard {
	return m.card
}

func (m *mockCallerCardStore) Remove() bool {
	had := m.card != nil
	m.card = nil
	return had
}

// boolPtr returns a pointer to a bool value. Used in tests for optional fields.
func boolPtr(v bool) *bool {
	return &v
}

// --- Mock Inbox ---

type mockInbox struct {
	mu      sync.Mutex
	entries []registry.InboxEntry
}

func (m *mockInbox) Deposit(entry registry.InboxEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	m.entries = append(m.entries, entry)
}

func (m *mockInbox) Peek(filter registry.InboxPeekFilter) []registry.InboxEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []registry.InboxEntry
	for _, e := range m.entries {
		if filter.Alias == "" || e.Alias == filter.Alias {
			result = append(result, e)
		}
	}
	return result
}

func (m *mockInbox) Pop(opts registry.InboxPopOptions) []registry.InboxEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	var matched, remaining []registry.InboxEntry
	for _, e := range m.entries {
		if e.Alias == opts.Alias {
			matched = append(matched, e)
		} else {
			remaining = append(remaining, e)
		}
	}
	m.entries = remaining
	return matched
}
