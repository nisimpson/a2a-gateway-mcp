package tool

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetHistoryTool retrieves the interaction history for a registered agent.
type GetHistoryTool struct {
	AgentRegistry  AgentRegistry
	HistoryBackend HistoryBackend
}

// NewGetHistoryTool creates a new GetHistoryTool using the agent registry and
// history backend from the provided environment.
func NewGetHistoryTool(env *Env) *GetHistoryTool {
	return &GetHistoryTool{
		AgentRegistry:  env.AgentRegistry,
		HistoryBackend: env.HistoryBackend,
	}
}

func (g *GetHistoryTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "get_history",
		Description: "Retrieve the interaction history for a connected agent. Returns a chronological list of past interactions including sent messages and responses.",
	}
}

func (g *GetHistoryTool) Handle(ctx context.Context, _ *mcp.CallToolRequest, input *GetHistoryInput) (*mcp.CallToolResult, *GetHistoryOutput, error) {
	if input.Agent == "" {
		return nil, nil, errors.New("agent alias is required")
	}

	if entry := g.AgentRegistry.Lookup(input.Agent); entry == nil {
		return nil, nil, errors.New("agent not found in registry")
	}

	entries, err := g.HistoryBackend.List(ctx, input.Agent)
	if err != nil {
		return nil, nil, err
	}

	// Apply limit if provided and positive — return the most recent N entries.
	if input.Limit != nil && *input.Limit > 0 && len(entries) > *input.Limit {
		entries = entries[len(entries)-*input.Limit:]
	}

	return nil, &GetHistoryOutput{Entries: entries}, nil
}

// ClearHistoryTool clears all interaction history for a registered agent.
type ClearHistoryTool struct {
	AgentRegistry  AgentRegistry
	HistoryBackend HistoryBackend
}

// NewClearHistoryTool creates a new ClearHistoryTool using the agent registry and
// history backend from the provided environment.
func NewClearHistoryTool(env *Env) *ClearHistoryTool {
	return &ClearHistoryTool{
		AgentRegistry:  env.AgentRegistry,
		HistoryBackend: env.HistoryBackend,
	}
}

func (c *ClearHistoryTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "clear_history",
		Description: "Clear all interaction history for a connected agent without disconnecting the agent.",
	}
}

func (c *ClearHistoryTool) Handle(ctx context.Context, _ *mcp.CallToolRequest, input *ClearHistoryInput) (*mcp.CallToolResult, *ClearHistoryOutput, error) {
	if input.Agent == "" {
		return nil, nil, errors.New("agent alias is required")
	}

	if entry := c.AgentRegistry.Lookup(input.Agent); entry == nil {
		return nil, nil, errors.New("agent not found in registry")
	}

	if err := c.HistoryBackend.Clear(ctx, input.Agent); err != nil {
		return nil, nil, err
	}

	return nil, &ClearHistoryOutput{Message: fmt.Sprintf("Cleared history for agent %q", input.Agent)}, nil
}
