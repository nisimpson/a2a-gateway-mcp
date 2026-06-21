package tool

import (
	"context"
	"errors"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nisimpson/a2a-gateway-mcp/internal/specification"
)

// GetTaskInput is the input schema for the get_task tool.
type GetTaskInput struct {
	Agent  string `json:"agent" jsonschema:"agent alias from registry or full HTTP/HTTPS URL"`
	TaskID string `json:"task_id" jsonschema:"the task identifier to retrieve"`
}

// GetTaskOutput is the output schema for the get_task tool.
type GetTaskOutput struct {
	ID        string `json:"id" jsonschema:"task identifier"`
	ContextID string `json:"contextId,omitempty" jsonschema:"conversation context identifier"`
	State     string `json:"state" jsonschema:"current task state (completed, working, input-required, failed, canceled)"`
	Response  string `json:"response,omitempty" jsonschema:"task response text extracted from artifacts or status message"`
}

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

	a2aClient, err := g.A2AClientResolver.Resolve(ctx, resolved)
	if err != nil {
		return nil, nil, handleA2AError(err)
	}

	task, err := a2aClient.GetTask(ctx, &a2a.GetTaskRequest{ID: a2a.TaskID(input.TaskID)})
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
