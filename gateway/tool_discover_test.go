package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Feature: a2a-gateway-mcp, Property 12: Directory discovery returns unmodified agent cards
// **Validates: Requirements AGMCP-16.1, AGMCP-16.5, AGMCP-16.6**

func TestPropertyDiscoverAgentsReturnsUnmodified(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for agent card names
	nameGen := gen.RegexMatch(`[a-z][a-z0-9]{1,10}`)

	// Generator for number of cards in the array (0-5)
	countGen := gen.IntRange(0, 5)

	properties.Property("discover_agents returns the exact JSON array from the directory without modification", prop.ForAll(
		func(names []string, count int) bool {
			// Limit to count cards
			if count > len(names) {
				count = len(names)
			}
			names = names[:count]

			// Build agent card objects
			cards := make([]map[string]interface{}, len(names))
			for i, name := range names {
				cards[i] = map[string]interface{}{
					"name":        name,
					"url":         fmt.Sprintf("https://%s.example.com", name),
					"description": fmt.Sprintf("Agent %s does things", name),
					"skills":      []string{fmt.Sprintf("skill-%d", i)},
				}
			}

			// Serialize the cards to JSON
			expectedJSON, err := json.Marshal(cards)
			if err != nil {
				return false
			}

			// Set up a test server that returns this JSON array
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(expectedJSON)
			}))
			defer ts.Close()

			// Create a gateway server and call discover_agents
			srv := NewServer(WithHTTPClient(ts.Client()))

			input := DiscoverAgentsInput{
				DirectoryURL: ts.URL,
			}

			result, _, err := srv.handleDiscoverAgents(context.Background(), &mcp.CallToolRequest{}, input)
			if err != nil {
				return false
			}

			if result.IsError {
				return false
			}

			// Extract the text content
			if len(result.Content) != 1 {
				return false
			}
			textContent, ok := result.Content[0].(*mcp.TextContent)
			if !ok {
				return false
			}

			// Parse both the expected and actual JSON and compare
			var expectedParsed, actualParsed interface{}
			if err := json.Unmarshal(expectedJSON, &expectedParsed); err != nil {
				return false
			}
			if err := json.Unmarshal([]byte(textContent.Text), &actualParsed); err != nil {
				return false
			}

			return reflect.DeepEqual(expectedParsed, actualParsed)
		},
		gen.SliceOfN(5, nameGen),
		countGen,
	))

	properties.TestingRun(t)
}

// Unit tests for discover_agents handler

func TestHandleDiscoverAgents_Success(t *testing.T) {
	// Set up a test server that returns a valid JSON array
	agentCards := `[{"name":"agent-1","url":"https://agent1.example.com"},{"name":"agent-2","url":"https://agent2.example.com"}]`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(agentCards))
	}))
	defer ts.Close()

	srv := NewServer(WithHTTPClient(ts.Client()))

	input := DiscoverAgentsInput{
		DirectoryURL: ts.URL,
	}

	result, _, err := srv.handleDiscoverAgents(context.Background(), &mcp.CallToolRequest{}, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}

	// Parse and compare
	var expected, actual interface{}
	if err := json.Unmarshal([]byte(agentCards), &expected); err != nil {
		t.Fatalf("failed to unmarshal expected: %v", err)
	}
	if err := json.Unmarshal([]byte(textContent.Text), &actual); err != nil {
		t.Fatalf("failed to unmarshal actual: %v", err)
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("response mismatch:\nexpected: %s\ngot: %s", agentCards, textContent.Text)
	}
}

func TestHandleDiscoverAgents_QueryAndLimitParams(t *testing.T) {
	var receivedQuery, receivedLimit string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.Query().Get("filter")
		receivedLimit = r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	srv := NewServer(WithHTTPClient(ts.Client()))

	limit := 5
	input := DiscoverAgentsInput{
		DirectoryURL: ts.URL,
		Filter:       "code review",
		Limit:        &limit,
	}

	result, _, err := srv.handleDiscoverAgents(context.Background(), &mcp.CallToolRequest{}, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	if receivedQuery != "code review" {
		t.Errorf("expected query param 'code review', got %q", receivedQuery)
	}
	if receivedLimit != "5" {
		t.Errorf("expected limit param '5', got %q", receivedLimit)
	}
}

func TestHandleDiscoverAgents_CustomHeaders(t *testing.T) {
	var receivedAuthHeader string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	srv := NewServer(WithHTTPClient(ts.Client()))

	input := DiscoverAgentsInput{
		DirectoryURL: ts.URL,
		Headers: map[string]string{
			"Authorization": "Bearer my-token",
		},
	}

	result, _, err := srv.handleDiscoverAgents(context.Background(), &mcp.CallToolRequest{}, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	if receivedAuthHeader != "Bearer my-token" {
		t.Errorf("expected Authorization header 'Bearer my-token', got %q", receivedAuthHeader)
	}
}

func TestHandleDiscoverAgents_ProtocolHeadersNotOverridden(t *testing.T) {
	var receivedAccept string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	srv := NewServer(WithHTTPClient(ts.Client()))

	input := DiscoverAgentsInput{
		DirectoryURL: ts.URL,
		Headers: map[string]string{
			"Accept": "text/html", // should be ignored
		},
	}

	result, _, err := srv.handleDiscoverAgents(context.Background(), &mcp.CallToolRequest{}, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	if receivedAccept != "application/json" {
		t.Errorf("expected Accept header 'application/json', got %q", receivedAccept)
	}
}

func TestHandleDiscoverAgents_InvalidURL(t *testing.T) {
	srv := NewServer()

	tests := []struct {
		name string
		url  string
	}{
		{"empty URL", ""},
		{"ftp scheme", "ftp://directory.example.com"},
		{"no scheme", "directory.example.com"},
		{"file scheme", "file:///etc/passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := DiscoverAgentsInput{
				DirectoryURL: tt.url,
			}

			result, _, err := srv.handleDiscoverAgents(context.Background(), &mcp.CallToolRequest{}, input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.IsError {
				t.Fatal("expected error result for invalid URL")
			}
		})
	}
}

func TestHandleDiscoverAgents_NonJSONResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>Not JSON</body></html>"))
	}))
	defer ts.Close()

	srv := NewServer(WithHTTPClient(ts.Client()))

	input := DiscoverAgentsInput{
		DirectoryURL: ts.URL,
	}

	result, _, err := srv.handleDiscoverAgents(context.Background(), &mcp.CallToolRequest{}, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for non-JSON response")
	}

	textContent := result.Content[0].(*mcp.TextContent)
	if !strings.Contains(textContent.Text, "not valid JSON") {
		t.Errorf("expected 'not valid JSON' in error, got: %s", textContent.Text)
	}
}

func TestHandleDiscoverAgents_NonArrayJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"agent-1","url":"https://agent1.example.com"}`))
	}))
	defer ts.Close()

	srv := NewServer(WithHTTPClient(ts.Client()))

	input := DiscoverAgentsInput{
		DirectoryURL: ts.URL,
	}

	result, _, err := srv.handleDiscoverAgents(context.Background(), &mcp.CallToolRequest{}, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for non-array JSON")
	}

	textContent := result.Content[0].(*mcp.TextContent)
	if !strings.Contains(textContent.Text, "not a JSON array") {
		t.Errorf("expected 'not a JSON array' in error, got: %s", textContent.Text)
	}
}

func TestHandleDiscoverAgents_UnreachableDirectory(t *testing.T) {
	// Use a short-timeout client to avoid waiting 30s in tests
	shortClient := &http.Client{Timeout: 1 * time.Second}
	srv := NewServer(WithHTTPClient(shortClient))

	input := DiscoverAgentsInput{
		DirectoryURL: "http://192.0.2.1:1", // non-routable address
	}

	result, _, err := srv.handleDiscoverAgents(context.Background(), &mcp.CallToolRequest{}, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for unreachable directory")
	}

	textContent := result.Content[0].(*mcp.TextContent)
	if !strings.Contains(textContent.Text, "unreachable") {
		t.Errorf("expected 'unreachable' in error, got: %s", textContent.Text)
	}
}

func TestHandleDiscoverAgents_TooManyHeaders(t *testing.T) {
	srv := NewServer()

	headers := make(map[string]string)
	for i := 0; i < 21; i++ {
		headers[fmt.Sprintf("X-Header-%d", i)] = fmt.Sprintf("value-%d", i)
	}

	input := DiscoverAgentsInput{
		DirectoryURL: "https://directory.example.com",
		Headers:      headers,
	}

	result, _, err := srv.handleDiscoverAgents(context.Background(), &mcp.CallToolRequest{}, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for too many headers")
	}

	textContent := result.Content[0].(*mcp.TextContent)
	if !strings.Contains(textContent.Text, "at most 20") {
		t.Errorf("expected 'at most 20' in error, got: %s", textContent.Text)
	}
}

func TestHandleDiscoverAgents_InvalidLimit(t *testing.T) {
	srv := NewServer()

	tests := []struct {
		name  string
		limit int
	}{
		{"zero limit", 0},
		{"negative limit", -1},
		{"very negative limit", -100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limit := tt.limit
			input := DiscoverAgentsInput{
				DirectoryURL: "https://directory.example.com",
				Limit:        &limit,
			}

			result, _, err := srv.handleDiscoverAgents(context.Background(), &mcp.CallToolRequest{}, input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.IsError {
				t.Fatal("expected error result for invalid limit")
			}

			textContent := result.Content[0].(*mcp.TextContent)
			if !strings.Contains(textContent.Text, "positive integer") {
				t.Errorf("expected 'positive integer' in error, got: %s", textContent.Text)
			}
		})
	}
}

func TestHandleDiscoverAgents_EmptyDirectoryURL(t *testing.T) {
	srv := NewServer()

	input := DiscoverAgentsInput{
		DirectoryURL: "",
	}

	result, _, err := srv.handleDiscoverAgents(context.Background(), &mcp.CallToolRequest{}, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for empty directory URL")
	}
}

func TestHandleDiscoverAgents_EmptyArray(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	srv := NewServer(WithHTTPClient(ts.Client()))

	input := DiscoverAgentsInput{
		DirectoryURL: ts.URL,
	}

	result, _, err := srv.handleDiscoverAgents(context.Background(), &mcp.CallToolRequest{}, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	textContent := result.Content[0].(*mcp.TextContent)
	if textContent.Text != "[]" {
		t.Errorf("expected '[]', got: %s", textContent.Text)
	}
}
