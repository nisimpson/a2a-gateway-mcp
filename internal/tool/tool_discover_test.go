package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nisimpson/a2a-gateway-mcp/directory"
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
	responseJSON := `{"cards":[{"name":"agent1","url":"http://a1.example.com"},{"name":"agent2","url":"http://a2.example.com"}]}`

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
				Body:       io.NopCloser(bytes.NewBufferString(`"just a string"`)),
				Header:     make(http.Header),
			}, nil
		},
	}

	d := newDiscoverAgentsTool(httpClient)
	result, output, err := d.Handle(context.Background(), nil, &DiscoverAgentsInput{
		DirectoryURL: "https://example.com/directory",
	})
	if err == nil {
		t.Fatal("expected error for non-QueryResult JSON")
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
	responseJSON := `{"cards":[{"name":"agent1","url":"http://a1.example.com"}]}`

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

func TestDiscover_SupportedInterfaces_BackfillsURL(t *testing.T) {
	// A2A spec v1.0 places the agent URL inside supportedInterfaces, not as a
	// flat "url" field. The discover tool must backfill URL from there.
	// Fixes: https://github.com/nisimpson/a2a-gateway-mcp/issues/35
	responseJSON := `{"cards":[{"name":"my-agent","description":"an agent","supportedInterfaces":[{"url":"https://example.com/agents/my-agent/a2a","protocolBinding":"jsonrpc"}]}]}`

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
	_, output, err := d.Handle(context.Background(), nil, &DiscoverAgentsInput{
		DirectoryURL: "https://example.com/directory",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output == nil {
		t.Fatal("expected non-nil output")
	}
	if len(output.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(output.Agents))
	}
	if output.Agents[0].URL != "https://example.com/agents/my-agent/a2a" {
		t.Errorf("expected URL from supportedInterfaces, got %q", output.Agents[0].URL)
	}
}

func TestDiscover_FlatURL_StillWorks(t *testing.T) {
	// Backward compatibility: if a flat "url" field is present, it should be used directly.
	responseJSON := `{"cards":[{"name":"legacy-agent","url":"https://legacy.example.com/agent"}]}`

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
	_, output, err := d.Handle(context.Background(), nil, &DiscoverAgentsInput{
		DirectoryURL: "https://example.com/directory",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.Agents[0].URL != "https://legacy.example.com/agent" {
		t.Errorf("expected flat URL preserved, got %q", output.Agents[0].URL)
	}
}

func TestDiscover_BothURLAndSupportedInterfaces_FlatWins(t *testing.T) {
	// If both a flat "url" and supportedInterfaces are present, the flat URL wins.
	// SupportedInterfaces must not appear in the output JSON.
	responseJSON := `{"cards":[{"name":"agent","url":"https://flat.example.com","supportedInterfaces":[{"url":"https://iface.example.com"}]}]}`

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
	_, output, err := d.Handle(context.Background(), nil, &DiscoverAgentsInput{
		DirectoryURL: "https://example.com/directory",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.Agents[0].URL != "https://flat.example.com" {
		t.Errorf("expected flat URL to take precedence, got %q", output.Agents[0].URL)
	}
	// Verify SupportedInterfaces is cleared (won't leak into output JSON).
	if output.Agents[0].SupportedInterfaces != nil {
		t.Error("expected SupportedInterfaces to be nil after backfill")
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
					// Return valid QueryResult JSON for non-help mode
					body := `{"cards":[]}`
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

// Feature: discover-agents-default-url, Property 3: Fallback priority chain
// **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 2.4**

// mockDirectoryQuerier records calls to Query and returns a canned response.
type mockDirectoryQuerier struct {
	Called bool
}

func (m *mockDirectoryQuerier) Query(ctx context.Context, filter string, limit int, cursor string, help bool) (*directory.QueryResult, error) {
	m.Called = true
	return &directory.QueryResult{
		Cards: []a2a.AgentCard{{Name: "in-process-agent"}},
	}, nil
}

func TestPropertyFallbackPriorityChain(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// We enumerate all 8 combinations of:
	// - explicit directory_url: present (non-empty) or absent (empty)
	// - self-hosted directory: non-nil or nil
	// - default directory URL: non-empty or empty
	//
	// For each combination, we verify which resolution path is selected.

	// Use a generator that picks an index from 0..7, representing the 8 combinations.
	combinationGen := gen.IntRange(0, 7)

	properties.Property("fallback priority chain selects the correct source for all 8 combinations", prop.ForAll(
		func(combo int) bool {
			// Decode combination bits:
			// bit 0: explicit URL present (1) or absent (0)
			// bit 1: directory non-nil (1) or nil (0)
			// bit 2: default URL non-empty (1) or empty (0)
			hasExplicit := (combo & 1) != 0
			hasDirectory := (combo & 2) != 0
			hasDefault := (combo & 4) != 0

			// Track which path was used
			var httpCalledURL string
			httpClient := &mockHTTPDoer{
				DoFn: func(req *http.Request) (*http.Response, error) {
					httpCalledURL = req.URL.String()
					body := `{"cards":[{"name":"http-agent"}]}`
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(bytes.NewBufferString(body)),
						Header:     make(http.Header),
					}, nil
				},
			}

			var dir *mockDirectoryQuerier
			if hasDirectory {
				dir = &mockDirectoryQuerier{}
			}

			defaultURL := ""
			if hasDefault {
				defaultURL = "https://default.example.com/agents"
			}

			tool := &DiscoverAgentsTool{
				HTTPClient:          httpClient,
				DefaultDirectoryURL: defaultURL,
				Directory:           DirectoryQuerier(nil),
			}
			if dir != nil {
				tool.Directory = dir
			}

			input := &DiscoverAgentsInput{}
			if hasExplicit {
				input.DirectoryURL = "https://explicit.example.com/agents"
			}

			_, _, err := tool.Handle(context.Background(), nil, input)

			// Determine expected behavior based on priority chain:
			// Priority 1: explicit URL → HTTP to explicit URL
			// Priority 2: self-hosted directory → in-process query
			// Priority 3: default URL → HTTP to default URL
			// Priority 4: none → error
			switch {
			case hasExplicit:
				// Priority 1: should use HTTP to explicit URL
				if err != nil {
					return false
				}
				if !strings.Contains(httpCalledURL, "explicit.example.com") {
					return false
				}
				// Directory should NOT have been called
				if dir != nil && dir.Called {
					return false
				}
				return true

			case hasDirectory:
				// Priority 2: should use in-process directory
				if err != nil {
					return false
				}
				if !dir.Called {
					return false
				}
				// HTTP should NOT have been called
				if httpCalledURL != "" {
					return false
				}
				return true

			case hasDefault:
				// Priority 3: should use HTTP to default URL
				if err != nil {
					return false
				}
				if !strings.Contains(httpCalledURL, "default.example.com") {
					return false
				}
				return true

			default:
				// Priority 4: no source → error
				if err == nil {
					return false
				}
				if !strings.Contains(err.Error(), "directory URL is required") {
					return false
				}
				return true
			}
		},
		combinationGen,
	))

	properties.TestingRun(t)
}

// --- Unit tests for schema variants and edge cases (Task 5.7) ---

// flexMockDirectoryQuerier is a more flexible mock that supports custom Query functions.
type flexMockDirectoryQuerier struct {
	QueryFn func(ctx context.Context, filter string, limit int, cursor string, help bool) (*directory.QueryResult, error)
}

func (m *flexMockDirectoryQuerier) Query(ctx context.Context, filter string, limit int, cursor string, help bool) (*directory.QueryResult, error) {
	return m.QueryFn(ctx, filter, limit, cursor, help)
}

// TestSchema_SelfHostedDirectory_OmitsDirectoryURL verifies that when a self-hosted
// directory is configured, the schema omits directory_url from properties entirely.
// Validates: Requirement 5.1
func TestSchema_SelfHostedDirectory_OmitsDirectoryURL(t *testing.T) {
	tool := &DiscoverAgentsTool{
		HTTPClient: &mockHTTPDoer{},
		Directory:  &mockDirectoryQuerier{},
	}

	mcpTool := tool.Tool()
	schema, ok := mcpTool.InputSchema.(*jsonschema.Schema)
	if !ok {
		t.Fatalf("expected *jsonschema.Schema, got %T", mcpTool.InputSchema)
	}

	// directory_url should NOT be in properties
	if _, exists := schema.Properties["directory_url"]; exists {
		t.Error("expected directory_url to be omitted from schema properties when self-hosted directory is configured")
	}

	// required should be empty
	if len(schema.Required) != 0 {
		t.Errorf("expected empty required array, got %v", schema.Required)
	}

	// Other properties should still exist
	for _, expectedProp := range []string{"filter", "limit", "cursor", "headers", "help"} {
		if _, exists := schema.Properties[expectedProp]; !exists {
			t.Errorf("expected property %q to exist in schema", expectedProp)
		}
	}
}

// TestSchema_DefaultURL_HasOptionalDirectoryURL verifies that when a default URL is
// configured (no self-hosted directory), directory_url is optional with a hint in the description.
// Validates: Requirements 5.2, 5.4
func TestSchema_DefaultURL_HasOptionalDirectoryURL(t *testing.T) {
	tool := &DiscoverAgentsTool{
		HTTPClient:          &mockHTTPDoer{},
		DefaultDirectoryURL: "https://default.example.com/agents",
	}

	mcpTool := tool.Tool()
	schema, ok := mcpTool.InputSchema.(*jsonschema.Schema)
	if !ok {
		t.Fatalf("expected *jsonschema.Schema, got %T", mcpTool.InputSchema)
	}

	// directory_url should be in properties
	dirURLProp, exists := schema.Properties["directory_url"]
	if !exists {
		t.Fatal("expected directory_url to be in schema properties when default URL is configured")
	}

	// directory_url should NOT be in required
	for _, req := range schema.Required {
		if req == "directory_url" {
			t.Error("expected directory_url to NOT be in required array when default URL is configured")
		}
	}

	// Description should hint about server default
	if !strings.Contains(dirURLProp.Description, "server default") {
		t.Errorf("expected directory_url description to contain 'server default', got %q", dirURLProp.Description)
	}
}

// TestSchema_NoDefault_RequiresDirectoryURL verifies that when neither a self-hosted
// directory nor a default URL is configured, directory_url is required.
// Validates: Requirement 5.3
func TestSchema_NoDefault_RequiresDirectoryURL(t *testing.T) {
	tool := &DiscoverAgentsTool{
		HTTPClient: &mockHTTPDoer{},
	}

	mcpTool := tool.Tool()
	schema, ok := mcpTool.InputSchema.(*jsonschema.Schema)
	if !ok {
		t.Fatalf("expected *jsonschema.Schema, got %T", mcpTool.InputSchema)
	}

	// directory_url should be in properties
	if _, exists := schema.Properties["directory_url"]; !exists {
		t.Fatal("expected directory_url to be in schema properties when no default is configured")
	}

	// directory_url should be in required
	found := false
	for _, req := range schema.Required {
		if req == "directory_url" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected directory_url to be in required array when no default is configured")
	}
}

// TestHandle_NoDirectorySource_ReturnsSpecificError verifies that when no source is
// available (no explicit URL, no directory, no default), a specific error message is returned.
// Validates: Requirement 3.4
func TestHandle_NoDirectorySource_ReturnsSpecificError(t *testing.T) {
	tool := &DiscoverAgentsTool{
		HTTPClient: &mockHTTPDoer{},
	}

	_, _, err := tool.Handle(context.Background(), nil, &DiscoverAgentsInput{})
	if err == nil {
		t.Fatal("expected error when no directory source is available")
	}

	expectedMsg := "directory URL is required: no explicit URL, self-hosted directory, or default URL configured"
	if err.Error() != expectedMsg {
		t.Errorf("expected error message %q, got %q", expectedMsg, err.Error())
	}
}

// TestHandle_SelfHosted_HelpTrue_ReturnsFilterHelpResponse verifies that a self-hosted
// directory query with help=true returns a FilterHelpResponse as text content.
// Validates: Requirement 4.5
func TestHandle_SelfHosted_HelpTrue_ReturnsFilterHelpResponse(t *testing.T) {
	helpResp := &directory.FilterHelpResponse{
		Description: "Test filter help",
		Syntax:      "plain text matching",
		Examples: []directory.FilterExample{
			{Filter: "weather", Description: "find weather agents"},
		},
		FilterableFields: []string{"name", "description"},
	}

	dir := &flexMockDirectoryQuerier{
		QueryFn: func(ctx context.Context, filter string, limit int, cursor string, help bool) (*directory.QueryResult, error) {
			if !help {
				t.Error("expected help=true to be passed to directory querier")
			}
			return &directory.QueryResult{
				HelpResp: helpResp,
			}, nil
		},
	}

	tool := &DiscoverAgentsTool{
		HTTPClient: &mockHTTPDoer{},
		Directory:  dir,
	}

	result, output, err := tool.Handle(context.Background(), nil, &DiscoverAgentsInput{
		Help: true,
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

	// Verify the JSON contains the help response fields
	var decoded directory.FilterHelpResponse
	if err := json.Unmarshal([]byte(tc.Text), &decoded); err != nil {
		t.Fatalf("failed to unmarshal help response: %v", err)
	}
	if decoded.Description != "Test filter help" {
		t.Errorf("expected description 'Test filter help', got %q", decoded.Description)
	}
	if decoded.Syntax != "plain text matching" {
		t.Errorf("expected syntax 'plain text matching', got %q", decoded.Syntax)
	}
	if len(decoded.Examples) != 1 {
		t.Errorf("expected 1 example, got %d", len(decoded.Examples))
	}
}

// TestHandle_SelfHosted_InternalError_PropagatesAsToolError verifies that when the
// self-hosted directory returns an error, it propagates as a tool error.
// Validates: Requirement 4.6
func TestHandle_SelfHosted_InternalError_PropagatesAsToolError(t *testing.T) {
	dir := &flexMockDirectoryQuerier{
		QueryFn: func(ctx context.Context, filter string, limit int, cursor string, help bool) (*directory.QueryResult, error) {
			return nil, errors.New("internal registry failure")
		},
	}

	tool := &DiscoverAgentsTool{
		HTTPClient: &mockHTTPDoer{},
		Directory:  dir,
	}

	_, _, err := tool.Handle(context.Background(), nil, &DiscoverAgentsInput{})
	if err == nil {
		t.Fatal("expected error when self-hosted directory returns an error")
	}
	if !strings.Contains(err.Error(), "directory query failed") {
		t.Errorf("expected error to contain 'directory query failed', got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "internal registry failure") {
		t.Errorf("expected error to contain original error message 'internal registry failure', got %q", err.Error())
	}
}
