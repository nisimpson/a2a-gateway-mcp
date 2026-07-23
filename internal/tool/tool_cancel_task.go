package tool

import (
	"context"
	"errors"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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

	err = resolveClientWithAuthRetry(ctx, c.A2AClientResolver, resolved, func(client *a2aclient.Client) error {
		_, cancelErr := client.CancelTask(ctx, &a2a.CancelTaskRequest{ID: a2a.TaskID(input.TaskID)})
		return cancelErr
	})
	if err != nil {
		return nil, nil, handleA2AError(err)
	}

	output := &CancelTaskOutput{
		Message: fmt.Sprintf("Task %s has been canceled", input.TaskID),
	}
	return nil, output, nil
}
