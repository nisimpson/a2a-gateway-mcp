package tool

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
)

func newDiscoverAgentsTool(httpClient *mockHTTPDoer) *DiscoverAgentsTool {
	return &DiscoverAgentsTool{
		HTTPClient: httpClient,
	}
}

func TestDiscover_InvalidURL(t *testing.T) {
	d := newDiscoverAgentsTool(&mockHTTPDoer{})
	result, output, err := d.Handle(context.Background(), nil, &DiscoverAgentsInput{
		DirectoryURL: "not-a-url",
	})
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
	if result != nil || output != nil {
		t.Fatal("expected nil result and output for validation error")
	}
}

func TestDiscover_InvalidLimit(t *testing.T) {
	d := newDiscoverAgentsTool(&mockHTTPDoer{})
	limit := 0
	result, output, err := d.Handle(context.Background(), nil, &DiscoverAgentsInput{
		DirectoryURL: "https://example.com/directory",
		Limit:        &limit,
	})
	if err == nil {
		t.Fatal("expected error for invalid limit")
	}
	if result != nil || output != nil {
		t.Fatal("expected nil result and output for validation error")
	}
}

func TestDiscover_Success(t *testing.T) {
	responseJSON := `[{"name":"agent1","url":"http://a1.example.com"},{"name":"agent2","url":"http://a2.example.com"}]`

	httpClient := &mockHTTPDoer{
		DoFn: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(responseJSON)),
				Header:     make(http.Header),
			}, nil
		},
	}

	d := newDiscoverAgentsTool(httpClient)
	result, output, err := d.Handle(context.Background(), nil, &DiscoverAgentsInput{
		DirectoryURL: "https://example.com/directory",
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
	if len(output.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(output.Agents))
	}
}

func TestDiscover_NotJSONArray(t *testing.T) {
	httpClient := &mockHTTPDoer{
		DoFn: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(`{"error":"not an array"}`)),
				Header:     make(http.Header),
			}, nil
		},
	}

	d := newDiscoverAgentsTool(httpClient)
	result, output, err := d.Handle(context.Background(), nil, &DiscoverAgentsInput{
		DirectoryURL: "https://example.com/directory",
	})
	if err == nil {
		t.Fatal("expected error for non-array JSON")
	}
	if result != nil || output != nil {
		t.Fatal("expected nil result and output for error")
	}
}

func TestDiscover_Unreachable(t *testing.T) {
	httpClient := &mockHTTPDoer{
		DoFn: func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}

	d := newDiscoverAgentsTool(httpClient)
	result, output, err := d.Handle(context.Background(), nil, &DiscoverAgentsInput{
		DirectoryURL: "https://example.com/directory",
	})
	if err == nil {
		t.Fatal("expected error for unreachable directory")
	}
	if result != nil || output != nil {
		t.Fatal("expected nil result and output for error")
	}
}
