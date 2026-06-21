package tool

import (
	"context"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetTaskInput is the input schema for the get_task tool.
type GetTaskInput struct {
	Agent  string `json:"agent" jsonschema:"agent alias from registry or full HTTP/HTTPS URL"`
	TaskID string `json:"task_id" jsonschema:"the task identifier to retrieve"`
}

// GetTaskTool retrieves the current state of a previously initiated task.
type GetTaskTool struct {
	AgentRegistry     AgentRegistry
	A2AClientResolver A2AClientResolver
}

func (g *GetTaskTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "get_task",
		Description: "Retrieve the current state of a previously initiated task from an A2A agent",
	}
}

func (g *GetTaskTool) Handle(ctx context.Context, _ *mcp.CallToolRequest, input *GetTaskInput) (*mcp.CallToolResult, any, error) {
	if input.Agent == "" {
		return toolError("agent identifier is required"), nil, nil
	}
	if input.TaskID == "" {
		return toolError("task_id is required"), nil, nil
	}

	resolved, err := g.AgentRegistry.ResolveAgent(input.Agent)
	if err != nil {
		return toolError(err.Error()), nil, nil
	}

	a2aClient, err := g.A2AClientResolver.Resolve(ctx, resolved)
	if err != nil {
		return handleA2AError(err), nil, nil
	}

	task, err := a2aClient.GetTask(ctx, &a2a.GetTaskRequest{ID: a2a.TaskID(input.TaskID)})
	if err != nil {
		return handleA2AError(err), nil, nil
	}

	switch task.Status.State {
	case a2a.TaskStateCompleted:
		result, _ := FormatTaskResponse(task)
		return result, nil, nil

	case a2a.TaskStateInputRequired:
		result, _ := FormatInputRequiredResponse(task)
		return result, nil, nil

	case a2a.TaskStateAuthRequired:
		result, _ := formatInterruptedResponse(task, "auth-required")
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
		result, _ := FormatTaskResponse(task)
		return result, nil, nil
	}
}
