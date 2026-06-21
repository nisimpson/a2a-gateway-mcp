package tool

import (
	"context"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CancelTaskInput is the input schema for the cancel_task tool.
type CancelTaskInput struct {
	Agent  string `json:"agent" jsonschema:"agent alias from registry or full HTTP/HTTPS URL"`
	TaskID string `json:"task_id" jsonschema:"the task identifier to cancel"`
}

// CancelTaskTool cancels a previously initiated task on an A2A agent.
type CancelTaskTool struct {
	AgentRegistry     AgentRegistry
	A2AClientResolver A2AClientResolver
}

func (c *CancelTaskTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "cancel_task",
		Description: "Cancel a running task on an A2A agent",
	}
}

func (c *CancelTaskTool) Handle(ctx context.Context, _ *mcp.CallToolRequest, input *CancelTaskInput) (*mcp.CallToolResult, any, error) {
	if input.Agent == "" {
		return toolError("agent identifier is required"), nil, nil
	}
	if input.TaskID == "" {
		return toolError("task_id is required"), nil, nil
	}

	resolved, err := c.AgentRegistry.ResolveAgent(input.Agent)
	if err != nil {
		return toolError(err.Error()), nil, nil
	}

	a2aClient, err := c.A2AClientResolver.Resolve(ctx, resolved)
	if err != nil {
		return handleA2AError(err), nil, nil
	}

	task, err := a2aClient.CancelTask(ctx, &a2a.CancelTaskRequest{ID: a2a.TaskID(input.TaskID)})
	if err != nil {
		return handleA2AError(err), nil, nil
	}

	if task.Status.State == a2a.TaskStateCanceled {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Task %s has been canceled", input.TaskID)}},
		}, nil, nil
	}

	// For other states, format by state (unexpected but handle gracefully).
	result, _ := FormatTaskResponse(task)
	return result, nil, nil
}
