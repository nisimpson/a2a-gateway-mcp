package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestConnectAgent_WithRateLimit verifies that connecting an agent with both
// rate_limit_rps and rate_limit_burst creates a rate limiter with the correct values.
// Requirements: RLIM-4.1, RLIM-4.2
func TestConnectAgent_WithRateLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer ts.Close()

	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	result := callTool(t, session, "connect_agent", map[string]any{
		"alias":            "rate-agent",
		"agent_url":        ts.URL,
		"rate_limit_rps":   10.0,
		"rate_limit_burst": 20,
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	// Verify the rate limiter was created with correct values.
	rps, burst, exists := srv.rateLimiters.Get("rate-agent")
	if !exists {
		t.Fatal("expected rate limiter to exist for 'rate-agent', got none")
	}
	if rps != 10.0 {
		t.Errorf("expected RPS 10.0, got %f", rps)
	}
	if burst != 20 {
		t.Errorf("expected burst 20, got %d", burst)
	}
}

// TestConnectAgent_OnlyRPS_Error verifies that providing only rate_limit_rps
// without rate_limit_burst returns a validation error.
// Requirements: RLIM-4.3
func TestConnectAgent_OnlyRPS_Error(t *testing.T) {
	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	result := callTool(t, session, "connect_agent", map[string]any{
		"alias":          "only-rps-agent",
		"agent_url":      "https://agent.example.com",
		"rate_limit_rps": 10.0,
	})

	if !result.IsError {
		t.Fatal("expected error when only rate_limit_rps is provided, got success")
	}

	text := getTextContent(t, result)
	expected := "rate_limit_rps and rate_limit_burst must both be provided together"
	if text != expected {
		t.Errorf("expected error message %q, got %q", expected, text)
	}
}

// TestConnectAgent_OnlyBurst_Error verifies that providing only rate_limit_burst
// without rate_limit_rps returns a validation error.
// Requirements: RLIM-4.3
func TestConnectAgent_OnlyBurst_Error(t *testing.T) {
	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	result := callTool(t, session, "connect_agent", map[string]any{
		"alias":            "only-burst-agent",
		"agent_url":        "https://agent.example.com",
		"rate_limit_burst": 20,
	})

	if !result.IsError {
		t.Fatal("expected error when only rate_limit_burst is provided, got success")
	}

	text := getTextContent(t, result)
	expected := "rate_limit_rps and rate_limit_burst must both be provided together"
	if text != expected {
		t.Errorf("expected error message %q, got %q", expected, text)
	}
}

// TestConnectAgent_ZeroRPS_DisablesRateLimit verifies that setting rate_limit_rps
// to zero disables rate limiting for that agent (no limiter created).
// Requirements: RLIM-4.5
func TestConnectAgent_ZeroRPS_DisablesRateLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer ts.Close()

	// Use a server with a global default rate limit so we can confirm it's overridden.
	srv := NewServer(WithRateLimit(5.0, 10))
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	result := callTool(t, session, "connect_agent", map[string]any{
		"alias":            "unlimited-agent",
		"agent_url":        ts.URL,
		"rate_limit_rps":   0.0,
		"rate_limit_burst": 10,
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	// Verify no rate limiter exists (disabled).
	_, _, exists := srv.rateLimiters.Get("unlimited-agent")
	if exists {
		t.Error("expected no rate limiter for zero-RPS agent, but one exists")
	}
}

// TestConnectAgent_Reconnect_ReplacesLimiter verifies that reconnecting an agent
// with a different rate limit config replaces the existing rate limiter.
// Requirements: RLIM-4.4
func TestConnectAgent_Reconnect_ReplacesLimiter(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer ts.Close()

	srv := NewServer()
	session := connectTestClient(t, srv.mcpServer)
	defer session.Close()

	// First connect with initial rate limit.
	result := callTool(t, session, "connect_agent", map[string]any{
		"alias":            "reconnect-agent",
		"agent_url":        ts.URL,
		"rate_limit_rps":   5.0,
		"rate_limit_burst": 10,
	})
	if result.IsError {
		t.Fatalf("first connect failed: %s", getTextContent(t, result))
	}

	// Verify initial config.
	rps, burst, exists := srv.rateLimiters.Get("reconnect-agent")
	if !exists {
		t.Fatal("expected rate limiter after first connect")
	}
	if rps != 5.0 || burst != 10 {
		t.Errorf("initial config: expected 5.0 rps / 10 burst, got %f / %d", rps, burst)
	}

	// Reconnect with different rate limit.
	result = callTool(t, session, "connect_agent", map[string]any{
		"alias":            "reconnect-agent",
		"agent_url":        ts.URL,
		"rate_limit_rps":   20.0,
		"rate_limit_burst": 50,
	})
	if result.IsError {
		t.Fatalf("reconnect failed: %s", getTextContent(t, result))
	}

	// Verify the rate limiter was replaced with new values.
	rps, burst, exists = srv.rateLimiters.Get("reconnect-agent")
	if !exists {
		t.Fatal("expected rate limiter after reconnect")
	}
	if rps != 20.0 {
		t.Errorf("expected RPS 20.0 after reconnect, got %f", rps)
	}
	if burst != 50 {
		t.Errorf("expected burst 50 after reconnect, got %d", burst)
	}
}
