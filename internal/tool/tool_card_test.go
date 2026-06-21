package tool

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/nisimpson/a2a-gateway-mcp/registry"
)

func newGetAgentCardTool(reg *mockRegistry, httpClient *mockHTTPDoer) *GetAgentCardTool {
	return &GetAgentCardTool{
		AgentRegistry: reg,
		HTTPClient:    httpClient,
	}
}

func TestGetAgentCard_EmptyAgent(t *testing.T) {
	g := newGetAgentCardTool(&mockRegistry{}, &mockHTTPDoer{})
	result, out, err := g.Handle(context.Background(), nil, &GetAgentCardInput{Agent: ""})
	if err == nil {
		t.Fatal("expected error for empty agent")
	}
	if result != nil {
		t.Fatalf("unexpected result: %v", result)
	}
	if out != nil {
		t.Fatalf("unexpected output: %v", out)
	}
	if err.Error() != "agent identifier is required and cannot be empty" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetAgentCard_AgentUnreachable(t *testing.T) {
	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*registry.ResolveResult, error) {
			return &registry.ResolveResult{URL: "http://unreachable.invalid", IsAlias: false}, nil
		},
	}
	httpClient := &mockHTTPDoer{
		DoFn: func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}

	g := newGetAgentCardTool(reg, httpClient)
	result, out, err := g.Handle(context.Background(), nil, &GetAgentCardInput{Agent: "http://unreachable.invalid"})
	if err == nil {
		t.Fatal("expected error for unreachable agent")
	}
	if result != nil {
		t.Fatalf("unexpected result: %v", result)
	}
	if out != nil {
		t.Fatalf("unexpected output: %v", out)
	}
	if !contains(err.Error(), "agent unreachable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetAgentCard_Success(t *testing.T) {
	cardJSON := `{"name":"test-agent","description":"A test agent","url":"http://example.com"}`

	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*registry.ResolveResult, error) {
			return &registry.ResolveResult{URL: "http://example.com", IsAlias: true, Alias: identifier}, nil
		},
	}
	httpClient := &mockHTTPDoer{
		DoFn: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(cardJSON)),
				Header:     make(http.Header),
			}, nil
		},
	}

	g := newGetAgentCardTool(reg, httpClient)
	result, out, err := g.Handle(context.Background(), nil, &GetAgentCardInput{Agent: "test-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatalf("unexpected result for success: %v", result)
	}
	if out == nil {
		t.Fatal("expected structured output")
	}
	if out.Name != "test-agent" {
		t.Errorf("expected name 'test-agent', got %q", out.Name)
	}
}

func TestGetAgentCard_InvalidJSON(t *testing.T) {
	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*registry.ResolveResult, error) {
			return &registry.ResolveResult{URL: "http://example.com", IsAlias: false}, nil
		},
	}
	httpClient := &mockHTTPDoer{
		DoFn: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString("this is not json")),
				Header:     make(http.Header),
			}, nil
		},
	}

	g := newGetAgentCardTool(reg, httpClient)
	result, out, err := g.Handle(context.Background(), nil, &GetAgentCardInput{Agent: "http://example.com"})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if result != nil {
		t.Fatalf("unexpected result: %v", result)
	}
	if out != nil {
		t.Fatalf("unexpected output: %v", out)
	}
	if !contains(err.Error(), "failed to parse agent card JSON") {
		t.Fatalf("unexpected error: %v", err)
	}
}
