package gateway

import (
	"context"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/nisimpson/a2a-gateway-mcp/gateway/api"
	"github.com/nisimpson/a2a-gateway-mcp/internal/tool"
)

// ConnectAgent registers a remote A2A agent with a friendly alias.
// This is the programmatic equivalent of the connect_agent MCP tool.
func (s *Server) ConnectAgent(ctx context.Context, input *api.ConnectAgentInput) (*api.ConnectAgentOutput, error) {
	t := tool.NewConnectAgentTool(s.toolEnv())
	_, out, err := t.Handle(ctx, nil, input)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DisconnectAgent removes a registered agent by alias from the gateway registry.
// This is the programmatic equivalent of the disconnect_agent MCP tool.
func (s *Server) DisconnectAgent(ctx context.Context, input *api.DisconnectAgentInput) (*api.DisconnectAgentOutput, error) {
	t := tool.NewDisconnectAgentTool(s.toolEnv())
	_, out, err := t.Handle(ctx, nil, input)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SendMessage sends a message to a connected A2A agent by alias or URL.
// This is the programmatic equivalent of the send_message MCP tool.
//
// On success or partial failure (e.g., task failed), both output and error may
// be non-nil. The output contains the structured response from the agent.
func (s *Server) SendMessage(ctx context.Context, input *api.SendMessageInput) (*api.SendMessageOutput, error) {
	t := tool.NewSendMessageTool(s.toolEnv())
	_, out, err := t.Handle(ctx, nil, input)
	return out, err
}

// GetTask retrieves the current state of a previously initiated task from an A2A agent.
// This is the programmatic equivalent of the get_task MCP tool.
//
// On terminal failure states (failed, canceled), both the task and an error are returned.
func (s *Server) GetTask(ctx context.Context, input *api.GetTaskInput) (*a2a.Task, error) {
	t := tool.NewGetTaskTool(s.toolEnv())
	_, out, err := t.Handle(ctx, nil, input)
	if out == nil {
		return nil, err
	}
	task, _ := out.(*a2a.Task)
	return task, err
}

// CancelTask cancels a running task on an A2A agent.
// This is the programmatic equivalent of the cancel_task MCP tool.
func (s *Server) CancelTask(ctx context.Context, input *api.CancelTaskInput) (*api.CancelTaskOutput, error) {
	t := tool.NewCancelTaskTool(s.toolEnv())
	_, out, err := t.Handle(ctx, nil, input)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DiscoverAgents queries a remote agent directory service and returns
// discovered agent cards. This is the programmatic equivalent of the
// discover_agents MCP tool.
func (s *Server) DiscoverAgents(ctx context.Context, input *api.DiscoverAgentsInput) (*api.DiscoverAgentsOutput, error) {
	t := tool.NewDiscoverAgentsTool(s.toolEnv())
	_, out, err := t.Handle(ctx, nil, input)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetAgentCard retrieves the agent card from an A2A agent to discover its
// capabilities. This is the programmatic equivalent of the get_agent_card
// MCP tool.
func (s *Server) GetAgentCard(ctx context.Context, input *api.GetAgentCardInput) (*api.GetAgentCardOutput, error) {
	t := tool.NewGetAgentCardTool(s.toolEnv())
	_, out, err := t.Handle(ctx, nil, input)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// toolEnv constructs the internal tool.Env from the server's dependencies.
// This is the same wiring used by registerToolsV2 for the MCP tool handlers.
func (s *Server) toolEnv() *tool.Env {
	var dir tool.DirectoryQuerier
	if s.directory != nil {
		dir = &directoryAdapter{dir: s.directory}
	}

	return &tool.Env{
		AgentRegistry:          s.registryAdapter(),
		HealthTracker:          s.healthTracker,
		RateLimiter:            s.rateLimiters,
		HistoryBackend:         s.historyBackendAdapter(),
		A2AClientResolver:      s.clients,
		CallerCardInjector:     s.callerCardStore,
		CallerCardStore:        s.callerCardStore,
		ContextStore:           s.contextStore,
		HistoryRecorder:        s.historyRecorderAdapter(),
		AgentCardFetcher:       s.agentCardFetcherAdapter(),
		HTTPDoer:               s.httpClient,
		PingStrategy:           s.pingStrategy,
		Inbox:                  s.inbox,
		EffectivePollTimeout:   s.effectivePollTimeout,
		EffectiveStreamTimeout: s.effectiveStreamTimeout,
		DefaultRateLimit:       s.toolDefaultRateLimit(),

		DefaultDirectoryURL:    s.defaultDirectoryURL,
		DefaultDirectoryURLErr: s.defaultDirectoryURLErr,
		Directory:              dir,
	}
}
