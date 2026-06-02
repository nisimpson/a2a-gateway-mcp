package gateway

import (
	"context"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// handleCancelTask cancels a previously initiated task on an A2A agent.
func (s *Server) handleCancelTask(ctx context.Context, _ *mcp.CallToolRequest, input CancelTaskInput) (*mcp.CallToolResult, any, error) {
	// Validate agent identifier is provided.
	if input.Agent == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "agent identifier is required"}},
		}, nil, nil
	}

	// Validate task_id is provided.
	if input.TaskID == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "task_id is required"}},
		}, nil, nil
	}

	// Resolve agent identifier (alias or URL).
	resolved, err := ResolveAgent(s.registry, input.Agent)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}

	// Resolve SDK client.
	a2aClient, err := s.clients.Resolve(ctx, resolved)
	if err != nil {
		return handleA2AError(err), nil, nil
	}

	// Call SDK client to cancel task.
	task, err := a2aClient.CancelTask(ctx, &a2a.CancelTaskRequest{ID: a2a.TaskID(input.TaskID)})
	if err != nil {
		return handleA2AError(err), nil, nil
	}

	// If the task state is canceled, return success confirmation.
	if task.Status.State == a2a.TaskStateCanceled {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Task %s has been canceled", input.TaskID)}},
		}, nil, nil
	}

	// For other states, return the task formatted by state (unexpected but handle gracefully).
	return FormatTaskResponse(task), nil, nil
}
