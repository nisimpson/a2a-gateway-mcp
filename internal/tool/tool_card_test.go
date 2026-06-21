package tool

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
)

func newGetAgentCardTool(reg *mockRegistry, httpClient *mockHTTPDoer) *GetAgentCardTool {
	return &GetAgentCardTool{
		AgentRegistry: reg,
		HTTPClient:    httpClient,
	}
}

func TestGetAgentCard_EmptyAgent(t *testing.T) {
	g := newGetAgentCardTool(&mockRegistry{}, &mockHTTPDoer{})
	result, _, err := g.Handle(context.Background(), nil, &GetAgentCardInput{Agent: ""})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	assertTextContains(t, result, "agent identifier is required")
}

func TestGetAgentCard_AgentUnreachable(t *testing.T) {
	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*ResolveResult, error) {
			return &ResolveResult{URL: "http://unreachable.invalid", IsAlias: false}, nil
		},
	}
	httpClient := &mockHTTPDoer{
		DoFn: func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}

	g := newGetAgentCardTool(reg, httpClient)
	result, _, err := g.Handle(context.Background(), nil, &GetAgentCardInput{Agent: "http://unreachable.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	assertTextContains(t, result, "agent unreachable")
}

func TestGetAgentCard_Success(t *testing.T) {
	cardJSON := `{"name":"test-agent","description":"A test agent","url":"http://example.com"}`

	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*ResolveResult, error) {
			return &ResolveResult{URL: "http://example.com", IsAlias: true, Alias: identifier}, nil
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
	result, _, err := g.Handle(context.Background(), nil, &GetAgentCardInput{Agent: "test-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}
	assertTextContains(t, result, `"name":"test-agent"`)
}

func TestGetAgentCard_InvalidJSON(t *testing.T) {
	reg := &mockRegistry{
		ResolveAgentFn: func(identifier string) (*ResolveResult, error) {
			return &ResolveResult{URL: "http://example.com", IsAlias: false}, nil
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
	result, _, err := g.Handle(context.Background(), nil, &GetAgentCardInput{Agent: "http://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result for invalid JSON")
	}
	assertTextContains(t, result, "failed to parse agent card JSON")
}
