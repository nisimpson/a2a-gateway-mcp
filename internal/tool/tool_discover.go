package tool

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
	"github.com/nisimpson/a2a-gateway-mcp/internal/validate"
)

// DiscoverAgentsInput is the input schema for the discover_agents tool.
type DiscoverAgentsInput struct {
	DirectoryURL string            `json:"directory_url" jsonschema:"HTTP or HTTPS URL of the agent directory service"`
	Filter       string            `json:"filter,omitempty" jsonschema:"free-text search filter passed to the directory (max 256 chars)"`
	Limit        *int              `json:"limit,omitempty" jsonschema:"maximum number of agent cards to return (min 1)"`
	Headers      map[string]string `json:"headers,omitempty" jsonschema:"optional HTTP headers for directory authentication (max 20 entries)"`
}

// DiscoverAgentsTool queries a remote agent directory service and returns
// the raw JSON array of agent cards.
type DiscoverAgentsTool struct {
	HTTPClient HTTPDoer
}

func (d *DiscoverAgentsTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "discover_agents",
		Description: "Discover available agents from a remote agent directory service",
	}
}

func (d *DiscoverAgentsTool) Handle(ctx context.Context, _ *mcp.CallToolRequest, input *DiscoverAgentsInput) (*mcp.CallToolResult, any, error) {
	if err := validate.URL(input.DirectoryURL); err != nil {
		return toolError(err.Error()), nil, nil
	}

	if input.Limit != nil && *input.Limit < 1 {
		return toolError("limit must be a positive integer (>= 1)"), nil, nil
	}

	if err := validate.ValidateHeaders(input.Headers); err != nil {
		return toolError(err.Error()), nil, nil
	}

	// Build the GET URL with query parameters.
	reqURL, err := url.Parse(input.DirectoryURL)
	if err != nil {
		return toolError(fmt.Sprintf("invalid directory URL: %v", err)), nil, nil
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
		return toolError(fmt.Sprintf("failed to create request: %v", err)), nil, nil
	}

	req.Header.Set("Accept", "application/json")

	// Apply provided headers (skip protocol headers).
	for k, v := range input.Headers {
		if isProtocolHeader(k) {
			continue
		}
		req.Header.Set(k, v)
	}

	resp, err := d.HTTPClient.Do(req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "context deadline exceeded") {
			return toolError("directory is unreachable: request timed out"), nil, nil
		}
		return toolError(fmt.Sprintf("directory is unreachable: %v", err)), nil, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return toolError(fmt.Sprintf("failed to read directory response: %v", err)), nil, nil
	}

	if !json.Valid(body) {
		return toolError("directory response is not valid JSON"), nil, nil
	}

	// Validate that the response is a JSON array.
	var arr []json.RawMessage
	if err := json.Unmarshal(body, &arr); err != nil {
		return toolError("directory response is not a JSON array"), nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, nil, nil
}

// isProtocolHeader returns true for headers that must not be overridden.
func isProtocolHeader(name string) bool {
	return strings.EqualFold(name, "Content-Type") || strings.EqualFold(name, "Accept")
}
