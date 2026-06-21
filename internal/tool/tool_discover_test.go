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
	result, _, err := d.Handle(context.Background(), nil, &DiscoverAgentsInput{
		DirectoryURL: "not-a-url",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	assertTextContains(t, result, "http or https scheme")
}

func TestDiscover_InvalidLimit(t *testing.T) {
	d := newDiscoverAgentsTool(&mockHTTPDoer{})
	limit := 0
	result, _, err := d.Handle(context.Background(), nil, &DiscoverAgentsInput{
		DirectoryURL: "https://example.com/directory",
		Limit:        &limit,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	assertTextContains(t, result, "limit must be a positive integer")
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
	result, _, err := d.Handle(context.Background(), nil, &DiscoverAgentsInput{
		DirectoryURL: "https://example.com/directory",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}
	assertTextContains(t, result, "agent1")
	assertTextContains(t, result, "agent2")
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
	result, _, err := d.Handle(context.Background(), nil, &DiscoverAgentsInput{
		DirectoryURL: "https://example.com/directory",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result for non-array JSON")
	}
	assertTextContains(t, result, "not a JSON array")
}

func TestDiscover_Unreachable(t *testing.T) {
	httpClient := &mockHTTPDoer{
		DoFn: func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}

	d := newDiscoverAgentsTool(httpClient)
	result, _, err := d.Handle(context.Background(), nil, &DiscoverAgentsInput{
		DirectoryURL: "https://example.com/directory",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result for unreachable directory")
	}
	assertTextContains(t, result, "unreachable")
}
