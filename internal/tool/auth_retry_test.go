package tool

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/nisimpson/a2a-gateway-mcp/registry"
)

func TestIsAuthError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"generic error", errors.New("something broke"), false},
		{"unauthenticated", a2a.ErrUnauthenticated, true},
		{"unauthorized", a2a.ErrUnauthorized, true},
		{"wrapped unauthenticated", &a2a.Error{Err: a2a.ErrUnauthenticated, Message: "bad token"}, true},
		{"wrapped unauthorized", &a2a.Error{Err: a2a.ErrUnauthorized, Message: "forbidden"}, true},
		{"task not found", a2a.ErrTaskNotFound, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAuthError(tt.err)
			if got != tt.want {
				t.Errorf("isAuthError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestResolveClientWithAuthRetry_SuccessNoRetry(t *testing.T) {
	var resolveCount int
	resolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
			resolveCount++
			return &a2aclient.Client{}, nil
		},
	}

	resolved := &registry.ResolveResult{URL: "http://example.com", IsAlias: true, Alias: "test"}
	err := resolveClientWithAuthRetry(context.Background(), resolver, resolved, func(client *a2aclient.Client) error {
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolveCount != 1 {
		t.Errorf("expected 1 resolve call, got %d", resolveCount)
	}
}

func TestResolveClientWithAuthRetry_NonAuthErrorNoRetry(t *testing.T) {
	var resolveCount int
	resolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
			resolveCount++
			return &a2aclient.Client{}, nil
		},
	}

	resolved := &registry.ResolveResult{URL: "http://example.com", IsAlias: true, Alias: "test"}
	expectedErr := errors.New("network timeout")
	err := resolveClientWithAuthRetry(context.Background(), resolver, resolved, func(client *a2aclient.Client) error {
		return expectedErr
	})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected original error, got: %v", err)
	}
	if resolveCount != 1 {
		t.Errorf("expected 1 resolve call (no retry for non-auth error), got %d", resolveCount)
	}
}

func TestResolveClientWithAuthRetry_AuthErrorEvictsAndRetries(t *testing.T) {
	var resolveCount int
	var evictCalled bool
	resolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
			resolveCount++
			return &a2aclient.Client{}, nil
		},
		EvictFn: func(url string) {
			evictCalled = true
			if url != "http://example.com" {
				t.Errorf("evict called with wrong URL: %s", url)
			}
		},
	}

	resolved := &registry.ResolveResult{URL: "http://example.com", IsAlias: true, Alias: "test"}
	var callCount int
	err := resolveClientWithAuthRetry(context.Background(), resolver, resolved, func(client *a2aclient.Client) error {
		callCount++
		if callCount == 1 {
			return a2a.ErrUnauthenticated
		}
		return nil // retry succeeds
	})
	if err != nil {
		t.Fatalf("expected nil error after successful retry, got: %v", err)
	}
	if !evictCalled {
		t.Error("expected Evict to be called on auth failure")
	}
	if resolveCount != 2 {
		t.Errorf("expected 2 resolve calls (initial + retry), got %d", resolveCount)
	}
	if callCount != 2 {
		t.Errorf("expected fn called twice, got %d", callCount)
	}
}

func TestResolveClientWithAuthRetry_AuthErrorRetryAlsoFails(t *testing.T) {
	resolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
			return &a2aclient.Client{}, nil
		},
		EvictFn: func(url string) {},
	}

	resolved := &registry.ResolveResult{URL: "http://example.com", IsAlias: true, Alias: "test"}
	err := resolveClientWithAuthRetry(context.Background(), resolver, resolved, func(client *a2aclient.Client) error {
		return a2a.ErrUnauthorized // always fails
	})
	if !errors.Is(err, a2a.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized after retry exhausted, got: %v", err)
	}
}

func TestGetTask_AuthRetryOn401(t *testing.T) {
	var requestCount atomic.Int32
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		req, _ := readJSONRPCRequest(r)
		if count == 1 {
			// First call: return unauthenticated error (code -31401).
			writeJSONRPCError(w, req.ID, -31401, "unauthenticated")
			return
		}
		// Second call: succeed.
		task := &a2a.Task{
			ID:     "task-retry",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
		}
		writeJSONRPCResult(w, req.ID, task)
	}))
	defer agent.Close()

	var evictCount int
	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*registry.ResolveResult, error) {
			return &registry.ResolveResult{URL: agent.URL, IsAlias: true, Alias: identifier}, nil
		},
	}
	clientResolver := &mockClientResolver{
		ResolveFn: func(ctx context.Context, resolved *registry.ResolveResult) (*a2aclient.Client, error) {
			return newTestClient(ctx, resolved.URL)
		},
		EvictFn: func(url string) {
			evictCount++
		},
	}

	g := newGetTaskTool(reg, clientResolver)
	_, output, err := g.Handle(context.Background(), nil, &GetTaskInput{
		Agent:  "test-agent",
		TaskID: "task-retry",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output == nil {
		t.Fatal("expected non-nil output after retry")
	}
	task := output.(*a2a.Task)
	if task.ID != "task-retry" {
		t.Errorf("expected task ID 'task-retry', got %s", task.ID)
	}
	if evictCount != 1 {
		t.Errorf("expected 1 evict call, got %d", evictCount)
	}
}

// writeJSONRPCError writes a JSON-RPC error response. The a2a-go SDK maps
// specific codes to sentinel errors: -31401 → ErrUnauthenticated,
// -31403 → ErrUnauthorized.
func writeJSONRPCError(w http.ResponseWriter, reqID string, code int, message string) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      reqID,
		"error":   map[string]any{"code": code, "message": message},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
