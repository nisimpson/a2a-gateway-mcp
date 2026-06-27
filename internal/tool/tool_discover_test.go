package tool

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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

func TestDiscover_HelpMode_ReturnsTextContent(t *testing.T) {
	helpJSON := `{"description":"test","syntax":"test","examples":[],"filterable_fields":[]}`

	httpClient := &mockHTTPDoer{
		DoFn: func(req *http.Request) (*http.Response, error) {
			// Verify that help=true is in the query params
			if req.URL.Query().Get("help") != "true" {
				t.Error("expected help=true query parameter")
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(helpJSON)),
				Header:     make(http.Header),
			}, nil
		},
	}

	d := newDiscoverAgentsTool(httpClient)
	result, output, err := d.Handle(context.Background(), nil, &DiscoverAgentsInput{
		DirectoryURL: "https://example.com/directory",
		Help:         true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output != nil {
		t.Fatal("expected nil output when help=true")
	}
	if result == nil {
		t.Fatal("expected non-nil CallToolResult when help=true")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected content in CallToolResult")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if !strings.Contains(tc.Text, "description") {
		t.Errorf("expected help JSON in text content, got: %s", tc.Text)
	}
}

func TestDiscover_HelpFalse_ReturnsAgentArray(t *testing.T) {
	responseJSON := `[{"name":"agent1","url":"http://a1.example.com"}]`

	httpClient := &mockHTTPDoer{
		DoFn: func(req *http.Request) (*http.Response, error) {
			// Verify that help is NOT in the query params
			if req.URL.Query().Get("help") != "" {
				t.Error("expected no help query parameter when Help is false")
			}
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
		Help:         false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatal("expected nil result when Help is false")
	}
	if output == nil {
		t.Fatal("expected non-nil output when Help is false")
	}
	if len(output.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(output.Agents))
	}
	if output.Agents[0].Name != "agent1" {
		t.Errorf("expected agent name 'agent1', got %q", output.Agents[0].Name)
	}
}

// Feature: directory-filter-help, Property 3: Help flag controls URL construction in discover tool
// **Validates: Requirements 5.2, 5.4**

func TestPropertyHelpFlagControlsURLConstruction(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for random HTTPS host names (e.g. "abc123.example.com")
	hostGen := gen.RegexMatch(`[a-z][a-z0-9]{2,10}\.(com|org|net|io)`)

	// Generator for optional path segments (e.g. "/agents" or "/v1/directory")
	pathGen := gen.OneConstOf("", "/agents", "/v1/directory", "/api/discover", "/dir")

	// Generator for random filter strings (non-empty)
	filterGen := gen.RegexMatch(`[a-zA-Z][a-zA-Z0-9 ]{0,20}`)

	// Generator for random positive limit values (1-100)
	limitGen := gen.IntRange(1, 100)

	properties.Property("when Help is true, request URL contains help=true and does NOT contain filter or limit", prop.ForAll(
		func(host string, path string, filter string, limit int) bool {
			var capturedURL *url.URL

			httpClient := &mockHTTPDoer{
				DoFn: func(req *http.Request) (*http.Response, error) {
					capturedURL = req.URL
					// Return valid JSON for help mode
					body := `{"description":"help","syntax":"test","examples":[]}`
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(bytes.NewBufferString(body)),
						Header:     make(http.Header),
					}, nil
				},
			}

			d := newDiscoverAgentsTool(httpClient)
			directoryURL := fmt.Sprintf("https://%s%s", host, path)

			_, _, err := d.Handle(context.Background(), nil, &DiscoverAgentsInput{
				DirectoryURL: directoryURL,
				Filter:       filter,
				Limit:        &limit,
				Help:         true,
			})
			if err != nil {
				return false
			}
			if capturedURL == nil {
				return false
			}

			query := capturedURL.Query()

			// Must contain help=true
			if query.Get("help") != "true" {
				return false
			}

			// Must NOT contain filter or limit
			if query.Has("filter") {
				return false
			}
			if query.Has("limit") {
				return false
			}

			return true
		},
		hostGen,
		pathGen,
		filterGen,
		limitGen,
	))

	properties.Property("when Help is false, request URL does NOT contain help=true and DOES contain filter/limit when provided", prop.ForAll(
		func(host string, path string, filter string, limit int) bool {
			var capturedURL *url.URL

			httpClient := &mockHTTPDoer{
				DoFn: func(req *http.Request) (*http.Response, error) {
					capturedURL = req.URL
					// Return valid JSON array for non-help mode
					body := `[]`
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(bytes.NewBufferString(body)),
						Header:     make(http.Header),
					}, nil
				},
			}

			d := newDiscoverAgentsTool(httpClient)
			directoryURL := fmt.Sprintf("https://%s%s", host, path)

			_, _, err := d.Handle(context.Background(), nil, &DiscoverAgentsInput{
				DirectoryURL: directoryURL,
				Filter:       filter,
				Limit:        &limit,
				Help:         false,
			})
			if err != nil {
				return false
			}
			if capturedURL == nil {
				return false
			}

			query := capturedURL.Query()

			// Must NOT contain help=true
			if query.Get("help") == "true" {
				return false
			}

			// When filter is non-empty, it should be in the URL
			if filter != "" {
				if !query.Has("filter") {
					return false
				}
				if query.Get("filter") != filter {
					return false
				}
			}

			// When limit is provided (non-nil), it should be in the URL
			expectedLimit := fmt.Sprintf("%d", limit)
			if !query.Has("limit") {
				return false
			}
			if query.Get("limit") != expectedLimit {
				return false
			}

			return true
		},
		hostGen,
		pathGen,
		filterGen,
		limitGen,
	))

	properties.TestingRun(t)
}
