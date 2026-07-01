package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nisimpson/a2a-gateway-mcp/internal/validate"
)

// DiscoverAgentsInput is the input schema for the discover_agents tool.
type DiscoverAgentsInput struct {
	DirectoryURL string            `json:"directory_url" jsonschema:"HTTP or HTTPS URL of the agent directory service"`
	Filter       string            `json:"filter,omitempty" jsonschema:"free-text search filter passed to the directory (max 256 chars)"`
	Limit        *int              `json:"limit,omitempty" jsonschema:"maximum number of agent cards to return (min 1)"`
	Cursor       string            `json:"cursor,omitempty" jsonschema:"cursor token for pagination, pass the next_token from previous response to get the next page of results"`
	Headers      map[string]string `json:"headers,omitempty" jsonschema:"optional HTTP headers for directory authentication (max 20 entries)"`
	Help         bool              `json:"help,omitempty" jsonschema:"if true, return filter help documentation instead of agent cards"`
}

// DiscoverAgentsOutput is the output schema for the discover_agents tool.
type DiscoverAgentsOutput struct {
	Agents    []a2a.AgentCard `json:"agents" jsonschema:"list of discovered agent cards from the directory"`
	NextToken string          `json:"next_token,omitempty" jsonschema:"cursor token for next page of results"`
}

// DiscoverAgentsTool queries a remote agent directory service and returns
// the raw JSON array of agent cards.
type DiscoverAgentsTool struct {
	HTTPClient             HTTPDoer
	DefaultDirectoryURL    string
	DefaultDirectoryURLErr error
	Directory              DirectoryQuerier
}

// NewDiscoverAgentsTool creates a new DiscoverAgentsTool using the HTTP client
// and directory configuration provided by the given environment.
func NewDiscoverAgentsTool(env *Env) *DiscoverAgentsTool {
	return &DiscoverAgentsTool{
		HTTPClient:             env.HTTPDoer,
		DefaultDirectoryURL:    env.DefaultDirectoryURL,
		DefaultDirectoryURLErr: env.DefaultDirectoryURLErr,
		Directory:              env.Directory,
	}
}

func (d *DiscoverAgentsTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "discover_agents",
		Description: "Discover available agents from a remote agent directory service",
		InputSchema: d.buildInputSchema(),
	}
}

// buildInputSchema constructs the input JSON schema dynamically based on configuration.
//
// Variant A (self-hosted directory): omits directory_url entirely.
// Variant B (default URL configured): includes directory_url as optional with hint.
// Variant C (no default): includes directory_url in required array.
func (d *DiscoverAgentsTool) buildInputSchema() *jsonschema.Schema {
	properties := map[string]*jsonschema.Schema{
		"filter": {
			Type:        "string",
			Description: "free-text search filter passed to the directory (max 256 chars)",
		},
		"limit": {
			Type:        "integer",
			Description: "maximum number of agent cards to return (min 1)",
		},
		"cursor": {
			Type:        "string",
			Description: "cursor token for pagination, pass the next_token from previous response to get the next page of results",
		},
		"headers": {
			Type:        "object",
			Description: "optional HTTP headers for directory authentication (max 20 entries)",
			AdditionalProperties: &jsonschema.Schema{
				Type: "string",
			},
		},
		"help": {
			Type:        "boolean",
			Description: "if true, return filter help documentation instead of agent cards",
		},
	}

	var required []string

	switch {
	case d.Directory != nil:
		// Variant A: self-hosted directory — omit directory_url entirely.

	case d.DefaultDirectoryURL != "":
		// Variant B: default URL configured — directory_url is optional with hint.
		properties["directory_url"] = &jsonschema.Schema{
			Type:        "string",
			Description: "HTTP or HTTPS URL of the agent directory service (uses server default when omitted)",
		}

	default:
		// Variant C: no default — directory_url is required.
		properties["directory_url"] = &jsonschema.Schema{
			Type:        "string",
			Description: "HTTP or HTTPS URL of the agent directory service",
		}
		required = []string{"directory_url"}
	}

	schema := &jsonschema.Schema{
		Type:       "object",
		Properties: properties,
		Required:   required,
	}

	return schema
}

func (d *DiscoverAgentsTool) Handle(ctx context.Context, _ *mcp.CallToolRequest, input *DiscoverAgentsInput) (*mcp.CallToolResult, *DiscoverAgentsOutput, error) {
	if input.Limit != nil && *input.Limit < 1 {
		return nil, nil, errors.New("limit must be a positive integer (>= 1)")
	}

	if err := validate.Headers(input.Headers); err != nil {
		return nil, nil, err
	}

	// Determine the resolution source based on fallback priority chain.
	switch {
	case input.DirectoryURL != "":
		// Priority 1: Explicit directory_url provided — use HTTP.
		if err := validate.URL(input.DirectoryURL); err != nil {
			return nil, nil, err
		}
		return d.handleHTTP(ctx, input, input.DirectoryURL)

	case d.Directory != nil:
		// Priority 2: Self-hosted directory configured — in-process query.
		return d.handleInProcess(ctx, input)

	case d.DefaultDirectoryURL != "":
		// Priority 3: Default directory URL configured — use HTTP.
		if d.DefaultDirectoryURLErr != nil {
			return nil, nil, d.DefaultDirectoryURLErr
		}
		return d.handleHTTP(ctx, input, d.DefaultDirectoryURL)

	default:
		// Priority 4: No source available.
		return nil, nil, errors.New("directory URL is required: no explicit URL, self-hosted directory, or default URL configured")
	}
}

// handleHTTP performs an HTTP GET to the given directory URL and returns the result.
func (d *DiscoverAgentsTool) handleHTTP(ctx context.Context, input *DiscoverAgentsInput, directoryURL string) (*mcp.CallToolResult, *DiscoverAgentsOutput, error) {
	// Build the GET URL with query parameters.
	reqURL, err := url.Parse(directoryURL)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid directory URL: %w", err)
	}

	q := reqURL.Query()

	// When help is requested, only append help=true; skip filter/limit/cursor.
	if input.Help {
		q.Set("help", "true")
	} else {
		if input.Filter != "" {
			q.Set("filter", input.Filter)
		}
		if input.Limit != nil {
			q.Set("limit", fmt.Sprintf("%d", *input.Limit))
		}
		if input.Cursor != "" {
			q.Set("cursor", input.Cursor)
		}
	}
	reqURL.RawQuery = q.Encode()

	// Create HTTP request with timeout.
	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(timeoutCtx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
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
			return nil, nil, errors.New("directory is unreachable: request timed out")
		}
		return nil, nil, fmt.Errorf("directory is unreachable: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read directory response: %w", err)
	}

	if !json.Valid(body) {
		return nil, nil, errors.New("directory response is not valid JSON")
	}

	// When help mode is active, return raw JSON as text content.
	if input.Help {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
		}, nil, nil
	}

	// Parse response as QueryResult (new format with cards + next_token).
	var qr struct {
		Cards     []a2a.AgentCard `json:"cards"`
		NextToken string          `json:"next_token,omitempty"`
	}
	if err := json.Unmarshal(body, &qr); err != nil {
		return nil, nil, errors.New("directory response is not a valid query result")
	}

	if qr.Cards == nil {
		qr.Cards = []a2a.AgentCard{}
	}

	output := &DiscoverAgentsOutput{
		Agents:    qr.Cards,
		NextToken: qr.NextToken,
	}
	return nil, output, nil
}

// handleInProcess queries the self-hosted directory via the DirectoryQuerier interface.
func (d *DiscoverAgentsTool) handleInProcess(ctx context.Context, input *DiscoverAgentsInput) (*mcp.CallToolResult, *DiscoverAgentsOutput, error) {
	limit := 0
	if input.Limit != nil {
		limit = *input.Limit
	}

	result, err := d.Directory.Query(ctx, input.Filter, limit, input.Cursor, input.Help)
	if err != nil {
		return nil, nil, fmt.Errorf("directory query failed: %w", err)
	}

	// When help mode is active, return raw JSON as text content.
	if input.Help && result.HelpResp != nil {
		helpJSON, err := json.Marshal(result.HelpResp)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal help response: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(helpJSON)}},
		}, nil, nil
	}

	// Pass through agent cards directly — no conversion needed.
	output := &DiscoverAgentsOutput{
		Agents:    result.Cards,
		NextToken: result.NextToken,
	}
	return nil, output, nil
}

// isProtocolHeader returns true for headers that must not be overridden.
func isProtocolHeader(name string) bool {
	return strings.EqualFold(name, "Content-Type") || strings.EqualFold(name, "Accept")
}
