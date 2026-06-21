package tool

import (
	"context"
	"testing"

	"github.com/nisimpson/a2a-gateway-mcp/registry"
)

func newListTool() (*ListAgentsTool, *mockRegistry, *mockRateLimiter, *mockHealthTracker) {
	reg := &mockRegistry{}
	rl := &mockRateLimiter{}
	ht := &mockHealthTracker{
		GetStatusFn: func(alias string) string { return "healthy" },
	}
	tool := &ListAgentsTool{
		AgentRegistry: reg,
		RateLimiter:   rl,
		HealthTracker: ht,
	}
	return tool, reg, rl, ht
}

func TestList_Empty(t *testing.T) {
	tool, reg, _, _ := newListTool()
	reg.ListFn = func() []*registry.RegisteredAgent { return nil }

	result, output, err := tool.Handle(context.Background(), nil, &ListAgentsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil result for success")
	}
	if output == nil {
		t.Fatal("expected non-nil output")
	}
	if len(output.Agents) != 0 {
		t.Errorf("expected empty agents list, got %d", len(output.Agents))
	}
}

func TestList_WithAgents(t *testing.T) {
	tool, reg, rl, ht := newListTool()

	reg.ListFn = func() []*registry.RegisteredAgent {
		return []*registry.RegisteredAgent{
			{Alias: "agent-one", URL: "http://one.example.com"},
			{Alias: "agent-two", URL: "http://two.example.com"},
		}
	}

	rl.GetFn = func(alias string) (float64, int, bool) {
		if alias == "agent-one" {
			return 5.0, 10, true
		}
		return 0, 0, false
	}

	ht.GetStatusFn = func(alias string) string { return "healthy" }
	ht.GetFailuresFn = func(alias string) (int, bool) { return 0, false }

	result, output, err := tool.Handle(context.Background(), nil, &ListAgentsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil result for success")
	}
	if output == nil {
		t.Fatal("expected non-nil output")
	}
	if len(output.Agents) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(output.Agents))
	}
	if output.Agents[0].Alias != "agent-one" {
		t.Errorf("expected alias 'agent-one', got %q", output.Agents[0].Alias)
	}
	if output.Agents[0].RateLimit != "5.00 rps, burst 10" {
		t.Errorf("expected rate limit '5.00 rps, burst 10', got %q", output.Agents[0].RateLimit)
	}
	if output.Agents[1].RateLimit != "unlimited" {
		t.Errorf("expected 'unlimited' for agent-two, got %q", output.Agents[1].RateLimit)
	}
	if output.Agents[0].Health != "healthy" {
		t.Errorf("expected health 'healthy', got %q", output.Agents[0].Health)
	}
}

func TestList_UnhealthyShowsFailures(t *testing.T) {
	tool, reg, rl, ht := newListTool()

	reg.ListFn = func() []*registry.RegisteredAgent {
		return []*registry.RegisteredAgent{
			{Alias: "sick-agent", URL: "http://sick.example.com"},
		}
	}

	rl.GetFn = func(alias string) (float64, int, bool) { return 0, 0, false }
	ht.GetStatusFn = func(alias string) string { return "unhealthy" }
	ht.GetFailuresFn = func(alias string) (int, bool) { return 5, true }

	result, output, err := tool.Handle(context.Background(), nil, &ListAgentsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil result for success")
	}
	if output == nil {
		t.Fatal("expected non-nil output")
	}
	if len(output.Agents) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(output.Agents))
	}
	if output.Agents[0].Health != "unhealthy" {
		t.Errorf("expected health 'unhealthy', got %q", output.Agents[0].Health)
	}
	if output.Agents[0].ConsecutiveFailures == nil {
		t.Fatal("expected consecutive_failures to be set")
	}
	if *output.Agents[0].ConsecutiveFailures != 5 {
		t.Errorf("expected 5 consecutive failures, got %d", *output.Agents[0].ConsecutiveFailures)
	}
}
