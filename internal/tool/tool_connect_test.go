package tool

import (
	"context"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

func newConnectTool() (*ConnectAgentTool, *mockRegistry, *mockClientResolver, *mockRateLimiter, *mockHealthTracker, *mockCardFetcher) {
	reg := &mockRegistry{}
	resolver := &mockClientResolver{}
	rl := &mockRateLimiter{}
	ht := &mockHealthTracker{}
	cf := &mockCardFetcher{}
	tool := &ConnectAgentTool{
		AgentRegistry:     reg,
		A2AClientResolver: resolver,
		ContextStore:      newMockContextStore(),
		RateLimiter:       rl,
		HealthTracker:     ht,
		CardFetcher:       cf,
	}
	return tool, reg, resolver, rl, ht, cf
}

func TestConnect_InvalidAlias(t *testing.T) {
	tool, _, _, _, _, _ := newConnectTool()
	result, output, err := tool.Handle(context.Background(), nil, &ConnectAgentInput{
		Alias:    "",
		AgentURL: "http://example.com",
	})
	if err == nil {
		t.Fatal("expected error for empty alias")
	}
	if result != nil || output != nil {
		t.Fatal("expected nil result and output for validation error")
	}
	if err.Error() != "alias is required and cannot be empty" {
		t.Fatalf("expected 'alias is required and cannot be empty', got %v", err)
	}
}

func TestConnect_InvalidURL(t *testing.T) {
	tool, _, _, _, _, _ := newConnectTool()
	result, output, err := tool.Handle(context.Background(), nil, &ConnectAgentInput{
		Alias:    "my-agent",
		AgentURL: "ftp://example.com",
	})
	if err == nil {
		t.Fatal("expected error for bad URL scheme")
	}
	if result != nil || output != nil {
		t.Fatal("expected nil result and output for validation error")
	}
	if err.Error() != "URL must have http or https scheme, got \"ftp\"" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestConnect_Success(t *testing.T) {
	tool, reg, _, _, ht, cf := newConnectTool()

	var connectCalled bool
	reg.ConnectFn = func(alias, url string, headers map[string]string, pingEndpoint string) bool {
		connectCalled = true
		return false
	}

	cf.FetchAgentCardFn = func(_ context.Context, _ string, _ map[string]string) *a2a.AgentCard {
		return &a2a.AgentCard{Name: "Test Agent"}
	}

	var setCardCalled bool
	reg.SetCardFn = func(alias string, card *a2a.AgentCard) bool {
		setCardCalled = true
		return true
	}

	var resetCalled bool
	ht.ResetFn = func(alias string) {
		resetCalled = true
	}

	result, output, err := tool.Handle(context.Background(), nil, &ConnectAgentInput{
		Alias:    "my-agent",
		AgentURL: "http://example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil result for success")
	}
	if output == nil {
		t.Fatal("expected non-nil output for success")
	}
	if output.Message == "" {
		t.Fatal("expected non-empty success message")
	}
	if !connectCalled {
		t.Error("expected Connect to be called")
	}
	if !setCardCalled {
		t.Error("expected SetCard to be called when card is fetched")
	}
	if !resetCalled {
		t.Error("expected HealthTracker.Reset to be called")
	}
}

func TestConnect_ReplacesExisting(t *testing.T) {
	tool, reg, resolver, _, _, _ := newConnectTool()

	// Existing agent at a different URL.
	reg.LookupFn = func(alias string) *AgentEntry {
		return &AgentEntry{Alias: "my-agent", URL: "http://old.com"}
	}

	var evictCalled bool
	resolver.EvictFn = func(url string) {
		evictCalled = true
		if url != "http://old.com" {
			t.Errorf("expected evict for old URL, got %s", url)
		}
	}

	reg.ConnectFn = func(alias, url string, headers map[string]string, pingEndpoint string) bool {
		return true
	}

	// Pre-seed context store.
	tool.ContextStore.Set("my-agent", "old-ctx")

	result, output, err := tool.Handle(context.Background(), nil, &ConnectAgentInput{
		Alias:    "my-agent",
		AgentURL: "http://new.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil result for success")
	}
	if output == nil {
		t.Fatal("expected non-nil output for success")
	}
	if !evictCalled {
		t.Error("expected Evict to be called for old URL")
	}
	// Context should be deleted because URL changed.
	if tool.ContextStore.Get("my-agent") != "" {
		t.Error("expected context store entry to be deleted on URL change")
	}
}

func TestConnect_RateLimitConfigured(t *testing.T) {
	tool, reg, _, rl, _, _ := newConnectTool()

	reg.ConnectFn = func(alias, url string, headers map[string]string, pingEndpoint string) bool {
		return false
	}

	var setAlias string
	var setRPS float64
	var setBurst int
	rl.SetFn = func(alias string, rps float64, burst int) {
		setAlias = alias
		setRPS = rps
		setBurst = burst
	}

	rps := 5.0
	burst := 10
	result, output, err := tool.Handle(context.Background(), nil, &ConnectAgentInput{
		Alias:          "my-agent",
		AgentURL:       "http://example.com",
		RateLimitRPS:   &rps,
		RateLimitBurst: &burst,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil result for success")
	}
	if output == nil {
		t.Fatal("expected non-nil output for success")
	}
	if setAlias != "my-agent" {
		t.Errorf("expected Set called with alias 'my-agent', got %q", setAlias)
	}
	if setRPS != 5.0 {
		t.Errorf("expected rps=5.0, got %f", setRPS)
	}
	if setBurst != 10 {
		t.Errorf("expected burst=10, got %d", setBurst)
	}
}

func TestConnect_DefaultRateLimitApplied(t *testing.T) {
	tool, reg, _, rl, _, _ := newConnectTool()

	reg.ConnectFn = func(alias, url string, headers map[string]string, pingEndpoint string) bool {
		return false
	}

	tool.DefaultRateLimit = RateLimitConfig{RequestsPerSecond: 2.0, Burst: 4}

	var setAlias string
	var setRPS float64
	var setBurst int
	rl.SetFn = func(alias string, rps float64, burst int) {
		setAlias = alias
		setRPS = rps
		setBurst = burst
	}

	result, output, err := tool.Handle(context.Background(), nil, &ConnectAgentInput{
		Alias:    "my-agent",
		AgentURL: "http://example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil result for success")
	}
	if output == nil {
		t.Fatal("expected non-nil output for success")
	}
	if setAlias != "my-agent" {
		t.Errorf("expected Set called with alias 'my-agent', got %q", setAlias)
	}
	if setRPS != 2.0 {
		t.Errorf("expected default rps=2.0, got %f", setRPS)
	}
	if setBurst != 4 {
		t.Errorf("expected default burst=4, got %d", setBurst)
	}
}
