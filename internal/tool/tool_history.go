package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetHistoryInput is the input schema for the get_history tool.
type GetHistoryInput struct {
	Agent string `json:"agent" jsonschema:"agent alias to retrieve history for"`
	Limit *int   `json:"limit,omitempty" jsonschema:"maximum number of recent entries to return"`
}

// GetHistoryTool retrieves the interaction history for a registered agent.
type GetHistoryTool struct {
	AgentRegistry  AgentRegistry
	HistoryBackend HistoryBackend
}

func (g *GetHistoryTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "get_history",
		Description: "Retrieve the interaction history for a connected agent. Returns a chronological list of past interactions including sent messages and responses.",
	}
}

func (g *GetHistoryTool) Handle(ctx context.Context, _ *mcp.CallToolRequest, input *GetHistoryInput) (*mcp.CallToolResult, any, error) {
	if input.Agent == "" {
		return toolError("agent alias is required"), nil, nil
	}

	if entry := g.AgentRegistry.Lookup(input.Agent); entry == nil {
		return toolError("agent not found in registry"), nil, nil
	}

	entries, err := g.HistoryBackend.List(ctx, input.Agent)
	if err != nil {
		return toolError(err.Error()), nil, nil
	}

	// Apply limit if provided and positive — return the most recent N entries.
	if input.Limit != nil && *input.Limit > 0 && len(entries) > *input.Limit {
		entries = entries[len(entries)-*input.Limit:]
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return toolError("failed to serialize history: " + err.Error()), nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

// ClearHistoryInput is the input schema for the clear_history tool.
type ClearHistoryInput struct {
	Agent string `json:"agent" jsonschema:"agent alias to clear history for"`
}

// ClearHistoryTool clears all interaction history for a registered agent.
type ClearHistoryTool struct {
	AgentRegistry  AgentRegistry
	HistoryBackend HistoryBackend
}

func (c *ClearHistoryTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "clear_history",
		Description: "Clear all interaction history for a connected agent without disconnecting the agent.",
	}
}

func (c *ClearHistoryTool) Handle(ctx context.Context, _ *mcp.CallToolRequest, input *ClearHistoryInput) (*mcp.CallToolResult, any, error) {
	if input.Agent == "" {
		return toolError("agent alias is required"), nil, nil
	}

	if entry := c.AgentRegistry.Lookup(input.Agent); entry == nil {
		return toolError("agent not found in registry"), nil, nil
	}

	if err := c.HistoryBackend.Clear(ctx, input.Agent); err != nil {
		return toolError(err.Error()), nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Cleared history for agent %q", input.Agent)}},
	}, nil, nil
}
