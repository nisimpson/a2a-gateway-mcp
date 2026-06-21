package tool

import (
	"context"
	"fmt"
	"testing"
	"time"
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
	result, output, err := tool.Handle(context.Background(), nil, &PingAgentInput{Alias: ""})
	if err == nil {
		t.Fatal("expected error for empty alias")
	}
	if result != nil || output != nil {
		t.Fatal("expected nil result and output for validation error")
	}
}

func TestPing_AgentNotFound(t *testing.T) {
	tool, reg, _, _ := newPingTool()
	reg.LookupFn = func(alias string) *AgentEntry { return nil }

	result, output, err := tool.Handle(context.Background(), nil, &PingAgentInput{Alias: "ghost"})
	if err == nil {
		t.Fatal("expected error for unknown alias")
	}
	if result != nil || output != nil {
		t.Fatal("expected nil result and output for error")
	}
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

	result, output, err := tool.Handle(context.Background(), nil, &PingAgentInput{Alias: "my-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil result for success")
	}
	if output == nil {
		t.Fatal("expected non-nil output")
	}
	if !successCalled {
		t.Error("expected RecordSuccess to be called")
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

	result, output, err := tool.Handle(context.Background(), nil, &PingAgentInput{Alias: "my-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil result for success")
	}
	if output == nil {
		t.Fatal("expected non-nil output")
	}
	if !failureCalled {
		t.Error("expected RecordFailure to be called")
	}
	if output.Reachable {
		t.Error("expected reachable=false")
	}
}
