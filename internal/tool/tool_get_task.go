package tool

import (
	"context"
	"errors"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nisimpson/a2a-gateway-mcp/internal/specification"
)

// GetTaskTool retrieves the current state of a previously initiated task.
type GetTaskTool struct {
	AgentRegistry     AgentRegistry
	A2AClientResolver A2AClientResolver
}

// NewGetTaskTool creates a new GetTaskTool using the agent registry and A2A client
// resolver from the provided environment.
func NewGetTaskTool(env *Env) *GetTaskTool {
	return &GetTaskTool{
		AgentRegistry:     env.AgentRegistry,
		A2AClientResolver: env.A2AClientResolver,
	}
}

func (g *GetTaskTool) Tool() *mcp.Tool {
	schema, _ := specification.TaskSchema()
	return &mcp.Tool{
		Name:         "get_task",
		Description:  "Retrieve the current state of a previously initiated task from an A2A agent",
		OutputSchema: schema,
	}
}

func (g *GetTaskTool) Handle(ctx context.Context, _ *mcp.CallToolRequest, input *GetTaskInput) (*mcp.CallToolResult, any, error) {
	if input.Agent == "" {
		return nil, nil, errors.New("agent identifier is required")
	}
	if input.TaskID == "" {
		return nil, nil, errors.New("task_id is required")
	}

	resolved, err := g.AgentRegistry.ResolveAgent(input.Agent)
	if err != nil {
		return nil, nil, err
	}

	var task *a2a.Task
	err = resolveClientWithAuthRetry(ctx, g.A2AClientResolver, resolved, func(client *a2aclient.Client) error {
		var getErr error
		task, getErr = client.GetTask(ctx, &a2a.GetTaskRequest{ID: a2a.TaskID(input.TaskID)})
		return getErr
	})
	if err != nil {
		return nil, nil, handleA2AError(err)
	}

	output := task

	switch task.Status.State {
	case a2a.TaskStateFailed:
		failMsg := "task failed"
		if task.Status.Message != nil {
			text := extractStatusMessageText(task.Status.Message)
			if text != "" {
				failMsg = text
			}
		}
		return nil, output, errors.New(failMsg)

	case a2a.TaskStateCanceled:
		return nil, output, errors.New("task was canceled")

	default:
		return nil, output, nil
	}
}
