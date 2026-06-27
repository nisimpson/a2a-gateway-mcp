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

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nisimpson/a2a-gateway-mcp/internal/validate"
)

// DiscoverAgentsInput is the input schema for the discover_agents tool.
type DiscoverAgentsInput struct {
	DirectoryURL string            `json:"directory_url" jsonschema:"HTTP or HTTPS URL of the agent directory service"`
	Filter       string            `json:"filter,omitempty" jsonschema:"free-text search filter passed to the directory (max 256 chars)"`
	Limit        *int              `json:"limit,omitempty" jsonschema:"maximum number of agent cards to return (min 1)"`
	Headers      map[string]string `json:"headers,omitempty" jsonschema:"optional HTTP headers for directory authentication (max 20 entries)"`
	Help         bool              `json:"help,omitempty" jsonschema:"if true, return filter help documentation instead of agent cards"`
}

// DiscoverAgentEntry describes a single agent card returned by discover_agents.
type DiscoverAgentEntry struct {
	Name        string   `json:"name" jsonschema:"agent display name"`
	Description string   `json:"description,omitempty" jsonschema:"agent description"`
	URL         string   `json:"url,omitempty" jsonschema:"agent URL"`
	Version     string   `json:"version,omitempty" jsonschema:"agent version"`
	Skills      []any    `json:"skills,omitempty" jsonschema:"agent skills"`
	InputModes  []string `json:"inputModes,omitempty" jsonschema:"supported input MIME types"`
	OutputModes []string `json:"outputModes,omitempty" jsonschema:"supported output MIME types"`
}

// DiscoverAgentsOutput is the output schema for the discover_agents tool.
type DiscoverAgentsOutput struct {
	Agents []DiscoverAgentEntry `json:"agents" jsonschema:"list of discovered agent cards from the directory"`
}

// DiscoverAgentsTool queries a remote agent directory service and returns
// the raw JSON array of agent cards.
type DiscoverAgentsTool struct {
	HTTPClient HTTPDoer
}

// NewDiscoverAgentsTool creates a new DiscoverAgentsTool using the HTTP client
// provided by the given environment configuration.
func NewDiscoverAgentsTool(env *Env) *DiscoverAgentsTool {
	return &DiscoverAgentsTool{HTTPClient: env.HTTPDoer}
}

func (d *DiscoverAgentsTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "discover_agents",
		Description: "Discover available agents from a remote agent directory service",
	}
}

func (d *DiscoverAgentsTool) Handle(ctx context.Context, _ *mcp.CallToolRequest, input *DiscoverAgentsInput) (*mcp.CallToolResult, *DiscoverAgentsOutput, error) {
	if err := validate.URL(input.DirectoryURL); err != nil {
		return nil, nil, err
	}

	if input.Limit != nil && *input.Limit < 1 {
		return nil, nil, errors.New("limit must be a positive integer (>= 1)")
	}

	if err := validate.Headers(input.Headers); err != nil {
		return nil, nil, err
	}

	// Build the GET URL with query parameters.
	reqURL, err := url.Parse(input.DirectoryURL)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid directory URL: %w", err)
	}

	q := reqURL.Query()

	// When help is requested, only append help=true; skip filter/limit.
	if input.Help {
		q.Set("help", "true")
	} else {
		if input.Filter != "" {
			q.Set("filter", input.Filter)
		}
		if input.Limit != nil {
			q.Set("limit", fmt.Sprintf("%d", *input.Limit))
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

	// Parse into output struct
	var agents []DiscoverAgentEntry
	if err := json.Unmarshal(body, &agents); err != nil {
		return nil, nil, errors.New("directory response is not a valid agent array")
	}

	output := &DiscoverAgentsOutput{Agents: agents}
	return nil, output, nil
}

// isProtocolHeader returns true for headers that must not be overridden.
func isProtocolHeader(name string) bool {
	return strings.EqualFold(name, "Content-Type") || strings.EqualFold(name, "Accept")
}
