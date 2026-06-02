package gateway

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ConnectAgentInput is the input schema for the connect_agent tool.
type ConnectAgentInput struct {
	Alias    string            `json:"alias" jsonschema:"short alias for the agent (lowercase alphanumeric and hyphens only, max 64 chars)"`
	AgentURL string            `json:"agent_url" jsonschema:"HTTP or HTTPS URL of the A2A agent"`
	Headers  map[string]string `json:"headers,omitempty" jsonschema:"optional HTTP headers to include on all requests to this agent (max 20 entries)"`
}

// DisconnectAgentInput is the input schema for the disconnect_agent tool.
type DisconnectAgentInput struct {
	Alias string `json:"alias" jsonschema:"alias of the agent to disconnect"`
}

// ListAgentsInput is the input schema for the list_agents tool (empty).
type ListAgentsInput struct{}

// GetAgentCardInput is the input schema for the get_agent_card tool.
type GetAgentCardInput struct {
	Agent string `json:"agent" jsonschema:"agent alias from registry or full HTTP/HTTPS URL"`
}

// SendMessageInput is the input schema for the send_message tool.
type SendMessageInput struct {
	Agent     string `json:"agent" jsonschema:"agent alias from registry or full HTTP/HTTPS URL"`
	Message   string `json:"message" jsonschema:"text message to send to the agent (max 32768 chars)"`
	ContextID string `json:"context_id,omitempty" jsonschema:"optional context ID to continue an existing conversation"`
	TaskID    string `json:"task_id,omitempty" jsonschema:"optional task ID to reference an existing task for follow-up messages"`
}

// BroadcastMessageInput is the input schema for the broadcast_message tool.
type BroadcastMessageInput struct {
	Aliases        []string `json:"aliases" jsonschema:"list of agent aliases to send the message to (min 1, max 20)"`
	Message        string   `json:"message" jsonschema:"text message to broadcast to all specified agents (max 32768 chars)"`
	TimeoutSeconds *int     `json:"timeout_seconds,omitempty" jsonschema:"per-agent timeout in seconds (min 1, max 120, default 30)"`
}

// GetTaskInput is the input schema for the get_task tool.
type GetTaskInput struct {
	Agent  string `json:"agent" jsonschema:"agent alias from registry or full HTTP/HTTPS URL"`
	TaskID string `json:"task_id" jsonschema:"the task identifier to retrieve"`
}

// CancelTaskInput is the input schema for the cancel_task tool.
type CancelTaskInput struct {
	Agent  string `json:"agent" jsonschema:"agent alias from registry or full HTTP/HTTPS URL"`
	TaskID string `json:"task_id" jsonschema:"the task identifier to cancel"`
}

// DiscoverAgentsInput is the input schema for the discover_agents tool.
type DiscoverAgentsInput struct {
	DirectoryURL string            `json:"directory_url" jsonschema:"HTTP or HTTPS URL of the agent directory service"`
	Filter       string            `json:"filter,omitempty" jsonschema:"free-text search filter passed to the directory (max 256 chars)"`
	Limit        *int              `json:"limit,omitempty" jsonschema:"maximum number of agent cards to return (min 1)"`
	Headers      map[string]string `json:"headers,omitempty" jsonschema:"optional HTTP headers for directory authentication (max 20 entries)"`
}

// registerTools registers all MCP tools with the server.
func (s *Server) registerTools() {
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "connect_agent",
		Description: "Register a remote A2A agent with a friendly alias for subsequent operations",
	}, s.handleConnectAgent)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "disconnect_agent",
		Description: "Remove a registered agent by alias from the gateway registry",
	}, s.handleDisconnectAgent)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "list_agents",
		Description: "List all currently connected agents with their aliases and URLs",
	}, s.handleListAgents)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "get_agent_card",
		Description: "Retrieve the agent card from an A2A agent to discover its capabilities",
	}, s.handleGetAgentCard)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "send_message",
		Description: "Send a text message to a connected A2A agent by alias or URL",
	}, s.handleSendMessage)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "get_task",
		Description: "Retrieve the current state of a previously initiated task from an A2A agent",
	}, s.handleGetTask)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "cancel_task",
		Description: "Cancel a running task on an A2A agent",
	}, s.handleCancelTask)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "broadcast_message",
		Description: "Send the same message to multiple agents simultaneously and collect responses",
	}, s.handleBroadcastMessage)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "discover_agents",
		Description: "Discover available agents from a remote agent directory service",
	}, s.handleDiscoverAgents)
}
