package gateway

import (
	"context"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// handleGetTask retrieves the current state of a previously initiated task.
func (s *Server) handleGetTask(ctx context.Context, _ *mcp.CallToolRequest, input GetTaskInput) (*mcp.CallToolResult, any, error) {
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

	// Call SDK client to get task.
	task, err := a2aClient.GetTask(ctx, &a2a.GetTaskRequest{ID: a2a.TaskID(input.TaskID)})
	if err != nil {
		return handleA2AError(err), nil, nil
	}

	// Format response based on task state.
	switch task.Status.State {
	case a2a.TaskStateCompleted:
		result, _ := FormatTaskResponse(task)
		return result, nil, nil

	case a2a.TaskStateInputRequired:
		result, _ := FormatInterruptedResponse(task, "input-required")
		return result, nil, nil

	case a2a.TaskStateAuthRequired:
		result, _ := FormatInterruptedResponse(task, "auth-required")
		return result, nil, nil

	case a2a.TaskStateFailed:
		failMsg := "task failed"
		if task.Status.Message != nil {
			text := extractStatusMessageText(task.Status.Message)
			if text != "" {
				failMsg = text
			}
		}
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: failMsg}},
		}, nil, nil

	case a2a.TaskStateCanceled:
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "task was canceled"}},
		}, nil, nil

	default:
		// For non-terminal states (working, submitted), show current state.
		result, _ := FormatTaskResponse(task)
		return result, nil, nil
	}
}
