package tool

import (
	"context"
	"errors"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CancelTaskInput is the input schema for the cancel_task tool.
type CancelTaskInput struct {
	Agent  string `json:"agent" jsonschema:"agent alias from registry or full HTTP/HTTPS URL"`
	TaskID string `json:"task_id" jsonschema:"the task identifier to cancel"`
}

// CancelTaskOutput is the output schema for the cancel_task tool.
type CancelTaskOutput struct {
	Message string `json:"message" jsonschema:"confirmation that the task was canceled"`
}

// CancelTaskTool cancels a previously initiated task on an A2A agent.
type CancelTaskTool struct {
	AgentRegistry     AgentRegistry
	A2AClientResolver A2AClientResolver
}

// NewCancelTaskTool creates a new CancelTaskTool using the registry and client
// resolver from the provided environment.
func NewCancelTaskTool(env *Env) *CancelTaskTool {
	return &CancelTaskTool{
		AgentRegistry:     env.AgentRegistry,
		A2AClientResolver: env.A2AClientResolver,
	}
}

func (c *CancelTaskTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "cancel_task",
		Description: "Cancel a running task on an A2A agent",
	}
}

func (c *CancelTaskTool) Handle(ctx context.Context, _ *mcp.CallToolRequest, input *CancelTaskInput) (*mcp.CallToolResult, *CancelTaskOutput, error) {
	if input.Agent == "" {
		return nil, nil, errors.New("agent identifier is required")
	}
	if input.TaskID == "" {
		return nil, nil, errors.New("task_id is required")
	}

	resolved, err := c.AgentRegistry.ResolveAgent(input.Agent)
	if err != nil {
		return nil, nil, err
	}

	a2aClient, err := c.A2AClientResolver.Resolve(ctx, resolved)
	if err != nil {
		return nil, nil, handleA2AError(err)
	}

	_, err = a2aClient.CancelTask(ctx, &a2a.CancelTaskRequest{ID: a2a.TaskID(input.TaskID)})
	if err != nil {
		return nil, nil, handleA2AError(err)
	}

	output := &CancelTaskOutput{
		Message: fmt.Sprintf("Task %s has been canceled", input.TaskID),
	}
	return nil, output, nil
}
