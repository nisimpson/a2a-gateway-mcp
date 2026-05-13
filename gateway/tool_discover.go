package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// handleDiscoverAgents queries a remote agent directory service and returns
// the raw JSON array of agent cards without modification.
// Requirement: AGMCP-16.1, AGMCP-16.5, AGMCP-16.6 — read-only directory discovery
func (s *Server) handleDiscoverAgents(ctx context.Context, _ *mcp.CallToolRequest, input DiscoverAgentsInput) (*mcp.CallToolResult, any, error) {
	// Validate directory_url is provided and has http/https scheme.
	if err := ValidateURL(input.DirectoryURL); err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}

	// Validate limit if provided: must be positive integer (>= 1).
	if input.Limit != nil && *input.Limit < 1 {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "limit must be a positive integer (>= 1)"}},
		}, nil, nil
	}

	// Validate headers count.
	if err := ValidateHeaders(input.Headers); err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}

	// Build the GET URL with query parameters.
	reqURL, err := url.Parse(input.DirectoryURL)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("invalid directory URL: %v", err)}},
		}, nil, nil
	}

	q := reqURL.Query()
	if input.Filter != "" {
		q.Set("filter", input.Filter)
	}
	if input.Limit != nil {
		q.Set("limit", fmt.Sprintf("%d", *input.Limit))
	}
	reqURL.RawQuery = q.Encode()

	// Create HTTP request with timeout.
	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(timeoutCtx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to create request: %v", err)}},
		}, nil, nil
	}

	// Set Accept header for JSON.
	req.Header.Set("Accept", "application/json")

	// Apply provided headers (skip protocol headers like Content-Type, Accept).
	for k, v := range input.Headers {
		if isProtocolHeader(k) {
			continue
		}
		req.Header.Set(k, v)
	}

	// Send the request.
	resp, err := s.httpClient.Do(req)
	if err != nil {
		// Provide a user-friendly error message.
		errMsg := err.Error()
		if strings.Contains(errMsg, "context deadline exceeded") {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "directory is unreachable: request timed out"}},
			}, nil, nil
		}
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("directory is unreachable: %v", err)}},
		}, nil, nil
	}
	defer resp.Body.Close()

	// Read the response body.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to read directory response: %v", err)}},
		}, nil, nil
	}

	// Validate that the response is valid JSON.
	if !json.Valid(body) {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "directory response is not valid JSON"}},
		}, nil, nil
	}

	// Validate that the response is a JSON array.
	var arr []json.RawMessage
	if err := json.Unmarshal(body, &arr); err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "directory response is not a JSON array"}},
		}, nil, nil
	}

	// Return the raw JSON array as MCP text content without modification.
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, nil, nil
}
