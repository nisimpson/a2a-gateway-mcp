package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func newPingTool() (*PingAgentTool, *mockRegistry, *mockHealthTracker, *mockPingStrategy) {
	reg := &mockRegistry{}
	ht := &mockHealthTracker{
		IsEnabledFn: func() bool { return true },
		GetStatusFn: func(alias string) string { return "healthy" },
	}
	ps := &mockPingStrategy{}
	tool := &PingAgentTool{
		AgentRegistry: reg,
		HealthTracker: ht,
		PingStrategy:  ps,
	}
	return tool, reg, ht, ps
}

func TestPing_EmptyAlias(t *testing.T) {
	tool, _, _, _ := newPingTool()
	result, _, err := tool.Handle(context.Background(), nil, &PingAgentInput{Alias: ""})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result for empty alias")
	}
	assertTextContains(t, result, "alias is required")
}

func TestPing_AgentNotFound(t *testing.T) {
	tool, reg, _, _ := newPingTool()
	reg.LookupFn = func(alias string) *AgentEntry { return nil }

	result, _, err := tool.Handle(context.Background(), nil, &PingAgentInput{Alias: "ghost"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result for unknown alias")
	}
	assertTextContains(t, result, "not found")
}

func TestPing_Reachable(t *testing.T) {
	tool, reg, ht, ps := newPingTool()

	reg.LookupFn = func(alias string) *AgentEntry {
		return &AgentEntry{Alias: "my-agent", URL: "http://example.com"}
	}

	ps.PingFn = func(_ context.Context, target PingTarget) PingResult {
		return PingResult{Reachable: true, ResponseTime: 42 * time.Millisecond}
	}

	var successCalled bool
	ht.RecordSuccessFn = func(alias string) {
		successCalled = true
	}

	result, _, err := tool.Handle(context.Background(), nil, &PingAgentInput{Alias: "my-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}
	if !successCalled {
		t.Error("expected RecordSuccess to be called")
	}

	// Parse JSON response.
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	var output PingAgentOutput
	if err := json.Unmarshal([]byte(tc.Text), &output); err != nil {
		t.Fatalf("failed to unmarshal ping response: %v", err)
	}
	if !output.Reachable {
		t.Error("expected reachable=true")
	}
	if output.ResponseTime == nil || *output.ResponseTime != 42 {
		t.Errorf("expected response_time_ms=42, got %v", output.ResponseTime)
	}
}

func TestPing_Unreachable(t *testing.T) {
	tool, reg, ht, ps := newPingTool()

	reg.LookupFn = func(alias string) *AgentEntry {
		return &AgentEntry{Alias: "my-agent", URL: "http://example.com"}
	}

	ps.PingFn = func(_ context.Context, target PingTarget) PingResult {
		return PingResult{Reachable: false, Err: fmt.Errorf("connection refused")}
	}

	var failureCalled bool
	ht.RecordFailureFn = func(alias string) {
		failureCalled = true
	}

	result, _, err := tool.Handle(context.Background(), nil, &PingAgentInput{Alias: "my-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %v", result.Content)
	}
	if !failureCalled {
		t.Error("expected RecordFailure to be called")
	}

	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	var output PingAgentOutput
	if err := json.Unmarshal([]byte(tc.Text), &output); err != nil {
		t.Fatalf("failed to unmarshal ping response: %v", err)
	}
	if output.Reachable {
		t.Error("expected reachable=false")
	}
}
