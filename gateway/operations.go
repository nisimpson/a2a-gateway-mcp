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

// PingAgent performs a liveness check on a registered agent.
// This is the programmatic equivalent of the ping_agent MCP tool.
func (s *Server) PingAgent(ctx context.Context, input *api.PingAgentInput) (*api.PingAgentOutput, error) {
	t := tool.NewPingAgentTool(s.toolEnv())
	_, out, err := t.Handle(ctx, nil, input)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// BroadcastMessage sends the same message to multiple agents simultaneously.
// This is the programmatic equivalent of the broadcast_message MCP tool.
//
// Returns a map of agent aliases to their individual SendMessageOutput results.
func (s *Server) BroadcastMessage(ctx context.Context, input *api.BroadcastMessageInput) (map[string]*api.SendMessageOutput, error) {
	t := tool.NewBroadcastMessageTool(s.toolEnv())
	_, out, err := t.Handle(ctx, nil, input)
	return out, err
}

// ListAgents returns all currently connected agents with their status.
// This is the programmatic equivalent of the list_agents MCP tool.
func (s *Server) ListAgents(ctx context.Context) (*api.ListAgentsOutput, error) {
	t := tool.NewListAgentsTool(s.toolEnv())
	_, out, err := t.Handle(ctx, nil, &api.ListAgentsInput{})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetHistory retrieves the interaction history for a connected agent.
// This is the programmatic equivalent of the get_history MCP tool.
func (s *Server) GetHistory(ctx context.Context, input *api.GetHistoryInput) (*api.GetHistoryOutput, error) {
	t := tool.NewGetHistoryTool(s.toolEnv())
	_, out, err := t.Handle(ctx, nil, input)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ClearHistory clears all interaction history for a connected agent.
// This is the programmatic equivalent of the clear_history MCP tool.
func (s *Server) ClearHistory(ctx context.Context, input *api.ClearHistoryInput) (*api.ClearHistoryOutput, error) {
	t := tool.NewClearHistoryTool(s.toolEnv())
	_, out, err := t.Handle(ctx, nil, input)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CreateCallerCard registers or replaces the caller agent card that is
// automatically included on all outbound messages.
// This is the programmatic equivalent of the create_caller_card MCP tool.
func (s *Server) CreateCallerCard(ctx context.Context, input *api.CreateCallerCardInput) (*api.CreateCallerCardOutput, error) {
	t := tool.NewCreateCallerCardTool(s.toolEnv())
	_, out, err := t.Handle(ctx, nil, input)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ViewCallerCard returns the currently registered caller agent card, if any.
// This is the programmatic equivalent of the view_caller_card MCP tool.
//
// Returns the card as raw JSON (any), or nil if no card is set.
func (s *Server) ViewCallerCard(ctx context.Context) (any, error) {
	t := tool.NewViewCallerCardTool(s.toolEnv())
	_, out, err := t.Handle(ctx, nil, &api.ViewCallerCardInput{})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RemoveCallerCard clears the stored caller agent card.
// This is the programmatic equivalent of the remove_caller_card MCP tool.
func (s *Server) RemoveCallerCard(ctx context.Context) (*api.RemoveCallerCardOutput, error) {
	t := tool.NewRemoveCallerCardTool(s.toolEnv())
	_, out, err := t.Handle(ctx, nil, &api.RemoveCallerCardInput{})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CheckInbox lists inbox entries without consuming them.
// This is the programmatic equivalent of the check_inbox MCP tool.
func (s *Server) CheckInbox(ctx context.Context, input *api.CheckInboxInput) (*api.CheckInboxOutput, error) {
	t := tool.NewCheckInboxTool(s.toolEnv())
	_, out, err := t.Handle(ctx, nil, input)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ReadInbox reads and consumes inbox messages for a specific agent.
// This is the programmatic equivalent of the read_inbox MCP tool.
func (s *Server) ReadInbox(ctx context.Context, input *api.ReadInboxInput) (*api.ReadInboxOutput, error) {
	t := tool.NewReadInboxTool(s.toolEnv())
	_, out, err := t.Handle(ctx, nil, input)
	if err != nil {
		return nil, err
	}
	return out, nil
}
