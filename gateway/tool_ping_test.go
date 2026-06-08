package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Feature: agent-health-checks, Property 12: Ping success resets across all HTTP status codes
// **Validates: Requirements HLTH-3.4, HLTH-8.2**

func TestPropertyPingAnyStatusResetsHealth(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	statusCodeGen := gen.IntRange(100, 599)

	properties.Property("any HTTP status code (100-599) from a ping results in (healthy, 0) state", prop.ForAll(
		func(statusCode int) bool {
			const alias = "test-agent"
			const threshold = 5

			tracker := NewHealthTracker(threshold)
			tracker.Reset(alias)
			for i := 0; i < threshold; i++ {
				tracker.RecordFailure(alias)
			}

			preState := tracker.Get(alias)
			if preState.Status != HealthStatusUnhealthy {
				return false
			}

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(statusCode)
			}))
			defer ts.Close()

			strategy := NewDefaultPingStrategy(ts.Client())
			target := PingTarget{
				Alias: alias,
				URL:   ts.URL,
			}

			result := strategy.Ping(context.Background(), target)
			if !result.Reachable {
				return false
			}

			tracker.RecordSuccess(alias)

			state := tracker.Get(alias)
			return state.Status == HealthStatusHealthy && state.Failures == 0
		},
		statusCodeGen,
	))

	properties.TestingRun(t)
}


// recordingPingStrategy is a test double for PingStrategy that returns a
// configurable PingResult and records the PingTarget and context for assertions.
type recordingPingStrategy struct {
	result     PingResult
	lastTarget PingTarget
	lastCtx    context.Context
}

func (m *recordingPingStrategy) Ping(ctx context.Context, target PingTarget) PingResult {
	m.lastCtx = ctx
	m.lastTarget = target
	return m.result
}

// newPingTestServer creates a Server with a mock ping strategy and a
// registered agent. The healthTracker uses threshold 3 and the agent
// is Reset in the tracker so health state exists.
func newPingTestServer(alias, agentURL string, mock *recordingPingStrategy) *Server {
	srv := NewServer(WithHealthCheck(HealthCheckOptions{
		FailureThreshold: 3,
		PingStrategy:     mock,
	}))
	srv.registry.Connect(alias, agentURL, nil, "")
	srv.healthTracker.Reset(alias)
	return srv
}

// --- Unit Tests for ping_agent handler (Task 4.4) ---
// Validates: Requirements HLTH-3.3, HLTH-3.6, HLTH-3.7, HLTH-3.8, HLTH-3.9, HLTH-3.10

func TestPingAgent_UnregisteredAlias(t *testing.T) {
	// Validates: HLTH-3.7 — unregistered alias returns error result
	mock := &recordingPingStrategy{}
	srv := NewServer(WithHealthCheck(HealthCheckOptions{
		FailureThreshold: 3,
		PingStrategy:     mock,
	}))

	input := PingAgentInput{Alias: "nonexistent"}
	result, _, err := srv.handlePingAgent(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for unregistered alias")
	}

	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent in error result")
	}
	if !strings.Contains(tc.Text, "not found") {
		t.Errorf("expected 'not found' in error message, got %q", tc.Text)
	}
}

func TestPingAgent_DefaultEndpoint(t *testing.T) {
	// Validates: HLTH-3.3 — when no PingEndpoint configured, target has empty
	// PingEndpoint (DefaultPingStrategy resolves to .well-known/agent.json)
	mock := &recordingPingStrategy{
		result: PingResult{Reachable: true, ResponseTime: 42 * time.Millisecond},
	}
	srv := newPingTestServer("my-agent", "http://agent.example.com", mock)

	input := PingAgentInput{Alias: "my-agent"}
	result, _, err := srv.handlePingAgent(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected success, got error")
	}

	// Verify the strategy received a target with empty PingEndpoint (default).
	if mock.lastTarget.PingEndpoint != "" {
		t.Errorf("expected empty PingEndpoint (default), got %q", mock.lastTarget.PingEndpoint)
	}
	if mock.lastTarget.URL != "http://agent.example.com" {
		t.Errorf("expected URL %q, got %q", "http://agent.example.com", mock.lastTarget.URL)
	}
	if mock.lastTarget.Alias != "my-agent" {
		t.Errorf("expected alias %q, got %q", "my-agent", mock.lastTarget.Alias)
	}
}

func TestPingAgent_CustomEndpoint(t *testing.T) {
	// Validates: HLTH-3.10 — custom ping endpoint is passed through to strategy
	mock := &recordingPingStrategy{
		result: PingResult{Reachable: true, ResponseTime: 10 * time.Millisecond},
	}
	srv := NewServer(WithHealthCheck(HealthCheckOptions{
		FailureThreshold: 3,
		PingStrategy:     mock,
	}))
	// Register agent with a custom ping endpoint.
	srv.registry.Connect("custom-agent", "http://agent.example.com", nil, "/health/live")
	srv.healthTracker.Reset("custom-agent")

	input := PingAgentInput{Alias: "custom-agent"}
	result, _, err := srv.handlePingAgent(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected success, got error")
	}

	// Verify the strategy received the custom PingEndpoint.
	if mock.lastTarget.PingEndpoint != "/health/live" {
		t.Errorf("expected PingEndpoint %q, got %q", "/health/live", mock.lastTarget.PingEndpoint)
	}
}

func TestPingAgent_5SecondTimeout(t *testing.T) {
	// Validates: HLTH-3.6 — 5-second timeout is applied to ping context
	mock := &recordingPingStrategy{
		result: PingResult{Reachable: true, ResponseTime: 1 * time.Millisecond},
	}
	srv := newPingTestServer("timeout-agent", "http://agent.example.com", mock)

	input := PingAgentInput{Alias: "timeout-agent"}
	_, _, err := srv.handlePingAgent(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the context passed to the strategy has a deadline.
	deadline, ok := mock.lastCtx.Deadline()
	if !ok {
		t.Fatal("expected context with deadline (5s timeout), but got none")
	}

	// The deadline should be approximately 5 seconds from when the handler ran.
	// Since the mock returns immediately, the remaining time should be close to 5s.
	remaining := time.Until(deadline)
	if remaining < 4*time.Second {
		t.Errorf("expected ~5s remaining on timeout, got %v", remaining)
	}
	if remaining > 6*time.Second {
		t.Errorf("expected ~5s timeout, but deadline is %v in the future", remaining)
	}
}

func TestPingAgent_SuccessResponseFormat(t *testing.T) {
	// Validates: HLTH-3.8 — success includes reachable=true, health status, response_time_ms
	mock := &recordingPingStrategy{
		result: PingResult{Reachable: true, ResponseTime: 123 * time.Millisecond},
	}
	srv := newPingTestServer("success-agent", "http://agent.example.com", mock)

	input := PingAgentInput{Alias: "success-agent"}
	result, _, err := srv.handlePingAgent(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected success result")
	}

	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}

	var resp pingResponse
	if err := json.Unmarshal([]byte(tc.Text), &resp); err != nil {
		t.Fatalf("failed to parse ping response JSON: %v", err)
	}

	if !resp.Reachable {
		t.Error("expected reachable=true")
	}
	if resp.Health != "healthy" {
		t.Errorf("expected health=%q (after success tracker records healthy), got %q", "healthy", resp.Health)
	}
	if resp.ResponseTime == nil {
		t.Fatal("expected response_time_ms to be present for successful ping")
	}
	if *resp.ResponseTime != 123 {
		t.Errorf("expected response_time_ms=123, got %d", *resp.ResponseTime)
	}
}

func TestPingAgent_FailureResponseFormat(t *testing.T) {
	// Validates: HLTH-3.9 — failure includes reachable=false, health status, no response_time_ms
	mock := &recordingPingStrategy{
		result: PingResult{Reachable: false, Err: errors.New("connection refused")},
	}
	srv := newPingTestServer("fail-agent", "http://agent.example.com", mock)

	input := PingAgentInput{Alias: "fail-agent"}
	result, _, err := srv.handlePingAgent(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected non-error MCP result (ping failure is reported in JSON body)")
	}

	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}

	var resp pingResponse
	if err := json.Unmarshal([]byte(tc.Text), &resp); err != nil {
		t.Fatalf("failed to parse ping response JSON: %v", err)
	}

	if resp.Reachable {
		t.Error("expected reachable=false")
	}
	if resp.Health == "" {
		t.Error("expected health field to be present")
	}
	if resp.ResponseTime != nil {
		t.Errorf("expected response_time_ms to be omitted for failure, got %d", *resp.ResponseTime)
	}
}

func TestPingAgent_SuccessUpdatesHealthTracker(t *testing.T) {
	// Validates: HLTH-3.4, HLTH-5.2 — successful ping resets health to healthy
	mock := &recordingPingStrategy{
		result: PingResult{Reachable: true, ResponseTime: 10 * time.Millisecond},
	}
	srv := newPingTestServer("recover-agent", "http://agent.example.com", mock)

	// Pre-condition: make agent unhealthy (3 failures).
	srv.healthTracker.RecordFailure("recover-agent")
	srv.healthTracker.RecordFailure("recover-agent")
	srv.healthTracker.RecordFailure("recover-agent")
	state := srv.healthTracker.Get("recover-agent")
	if state.Status != HealthStatusUnhealthy {
		t.Fatalf("pre-condition failed: expected unhealthy, got %s", state.Status)
	}

	// Ping succeeds → should recover to healthy.
	input := PingAgentInput{Alias: "recover-agent"}
	_, _, err := srv.handlePingAgent(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state = srv.healthTracker.Get("recover-agent")
	if state.Status != HealthStatusHealthy {
		t.Errorf("expected healthy after successful ping, got %s", state.Status)
	}
	if state.Failures != 0 {
		t.Errorf("expected 0 failures after successful ping, got %d", state.Failures)
	}
}

func TestPingAgent_FailureIncrementsFailureCount(t *testing.T) {
	// Validates: HLTH-3.5 — failed ping increments failure count
	mock := &recordingPingStrategy{
		result: PingResult{Reachable: false, Err: errors.New("connection refused")},
	}
	srv := newPingTestServer("failing-agent", "http://agent.example.com", mock)

	input := PingAgentInput{Alias: "failing-agent"}
	_, _, err := srv.handlePingAgent(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := srv.healthTracker.Get("failing-agent")
	if state.Failures != 1 {
		t.Errorf("expected 1 failure after failed ping, got %d", state.Failures)
	}
}

func TestPingAgent_HeadersPassedToStrategy(t *testing.T) {
	// Verify that per-agent headers are copied and passed to the strategy.
	mock := &recordingPingStrategy{
		result: PingResult{Reachable: true, ResponseTime: 5 * time.Millisecond},
	}
	srv := NewServer(WithHealthCheck(HealthCheckOptions{
		FailureThreshold: 3,
		PingStrategy:     mock,
	}))
	headers := map[string]string{
		"Authorization": "Bearer secret",
		"X-Custom":      "value",
	}
	srv.registry.Connect("header-agent", "http://agent.example.com", headers, "")
	srv.healthTracker.Reset("header-agent")

	input := PingAgentInput{Alias: "header-agent"}
	_, _, err := srv.handlePingAgent(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.lastTarget.Headers["Authorization"] != "Bearer secret" {
		t.Errorf("expected Authorization header, got %q", mock.lastTarget.Headers["Authorization"])
	}
	if mock.lastTarget.Headers["X-Custom"] != "value" {
		t.Errorf("expected X-Custom header, got %q", mock.lastTarget.Headers["X-Custom"])
	}
}

func TestPingAgent_InvalidAlias(t *testing.T) {
	// Validates alias validation rejects invalid formats.
	mock := &recordingPingStrategy{}
	srv := NewServer(WithHealthCheck(HealthCheckOptions{
		FailureThreshold: 3,
		PingStrategy:     mock,
	}))

	tests := []struct {
		name  string
		alias string
	}{
		{"empty alias", ""},
		{"uppercase", "MyAgent"},
		{"spaces", "my agent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := PingAgentInput{Alias: tt.alias}
			result, _, err := srv.handlePingAgent(context.Background(), nil, input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.IsError {
				t.Errorf("expected error for alias %q, got success", tt.alias)
			}
		})
	}
}
