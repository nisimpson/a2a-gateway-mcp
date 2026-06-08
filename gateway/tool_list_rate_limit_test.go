package gateway

import (
	"context"
	"encoding/json"
	"testing"
)

// TestListAgents_RateLimitedAgentsShowConfigValues verifies that list_agents
// shows the correct rate limit configuration for agents with rate limiters.
// Requirements: RLIM-6.1, RLIM-6.2
func TestListAgents_RateLimitedAgentsShowConfigValues(t *testing.T) {
	srv := NewServer()
	srv.registry.Connect("agent-a", "http://localhost:9001", nil, "")
	srv.registry.Connect("agent-b", "http://localhost:9002", nil, "")

	// Set rate limiters with specific config values.
	srv.rateLimiters.Set("agent-a", 10.0, 20)
	srv.rateLimiters.Set("agent-b", 5.5, 8)

	result, _, err := srv.handleListAgents(context.Background(), nil, ListAgentsInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error")
	}

	var entries []listAgentEntry
	text := extractText(t, result)
	if err := json.Unmarshal([]byte(text), &entries); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Entries are sorted by alias, so agent-a comes first.
	expected := map[string]string{
		"agent-a": "10.00 rps, burst 20",
		"agent-b": "5.50 rps, burst 8",
	}

	for _, entry := range entries {
		want, ok := expected[entry.Alias]
		if !ok {
			t.Errorf("unexpected alias: %s", entry.Alias)
			continue
		}
		if entry.RateLimit != want {
			t.Errorf("agent %s: expected rate_limit %q, got %q", entry.Alias, want, entry.RateLimit)
		}
	}
}

// TestListAgents_UnlimitedAgentsShowUnlimited verifies that list_agents
// shows "unlimited" for agents without rate limiters configured.
// Requirements: RLIM-6.1, RLIM-6.2
func TestListAgents_UnlimitedAgentsShowUnlimited(t *testing.T) {
	srv := NewServer()
	srv.registry.Connect("free-agent-1", "http://localhost:8001", nil, "")
	srv.registry.Connect("free-agent-2", "http://localhost:8002", nil, "")

	// No rate limiters set — all agents should be "unlimited".

	result, _, err := srv.handleListAgents(context.Background(), nil, ListAgentsInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error")
	}

	var entries []listAgentEntry
	text := extractText(t, result)
	if err := json.Unmarshal([]byte(text), &entries); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	for _, entry := range entries {
		if entry.RateLimit != "unlimited" {
			t.Errorf("agent %s: expected rate_limit %q, got %q", entry.Alias, "unlimited", entry.RateLimit)
		}
	}
}

// TestListAgents_MixedRateLimitAndUnlimited verifies that list_agents
// correctly displays rate limit values for agents with limiters and
// "unlimited" for agents without, in a single response.
// Requirements: RLIM-6.1, RLIM-6.2
func TestListAgents_MixedRateLimitAndUnlimited(t *testing.T) {
	srv := NewServer()
	srv.registry.Connect("limited-agent", "http://localhost:7001", nil, "")
	srv.registry.Connect("unlimited-agent", "http://localhost:7002", nil, "")
	srv.registry.Connect("another-limited", "http://localhost:7003", nil, "")

	// Set rate limiters only for some agents.
	srv.rateLimiters.Set("limited-agent", 10.0, 20)
	srv.rateLimiters.Set("another-limited", 2.0, 5)
	// "unlimited-agent" has no rate limiter.

	result, _, err := srv.handleListAgents(context.Background(), nil, ListAgentsInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error")
	}

	var entries []listAgentEntry
	text := extractText(t, result)
	if err := json.Unmarshal([]byte(text), &entries); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	expected := map[string]string{
		"limited-agent":   "10.00 rps, burst 20",
		"unlimited-agent": "unlimited",
		"another-limited": "2.00 rps, burst 5",
	}

	for _, entry := range entries {
		want, ok := expected[entry.Alias]
		if !ok {
			t.Errorf("unexpected alias: %s", entry.Alias)
			continue
		}
		if entry.RateLimit != want {
			t.Errorf("agent %s: expected rate_limit %q, got %q", entry.Alias, want, entry.RateLimit)
		}
	}
}
