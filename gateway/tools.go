package gateway

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ConnectAgentInput is the input schema for the connect_agent tool.
type ConnectAgentInput struct {
	Alias          string            `json:"alias" jsonschema:"short alias for the agent (lowercase alphanumeric and hyphens only, max 64 chars)"`
	AgentURL       string            `json:"agent_url" jsonschema:"HTTP or HTTPS URL of the A2A agent"`
	Headers        map[string]string `json:"headers,omitempty" jsonschema:"optional HTTP headers to include on all requests to this agent (max 20 entries)"`
	RateLimitRPS   *float64          `json:"rate_limit_rps,omitempty" jsonschema:"requests per second rate limit for this agent (must be provided with rate_limit_burst)"`
	RateLimitBurst *int              `json:"rate_limit_burst,omitempty" jsonschema:"burst capacity for this agent's rate limiter (must be provided with rate_limit_rps)"`
	PingEndpoint   *string           `json:"ping_endpoint,omitempty" jsonschema:"relative URL path for liveness checks (starts with /, max 256 chars)"`
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

// InputPart represents a single content part in a multi-part message.
// Exactly one of Text, Data, URL, or Raw should be set.
type InputPart struct {
	// Text contains plain text content.
	Text *string `json:"text,omitempty" jsonschema:"plain text content"`
	// Data contains structured JSON data to send as a DataPart.
	Data any `json:"data,omitempty" jsonschema:"structured JSON data (object, array, or primitive)"`
	// URL contains a URL reference to send as a URLPart.
	URL *string `json:"url,omitempty" jsonschema:"URL reference"`
	// Raw contains base64-encoded binary data to send as a RawPart.
	Raw *string `json:"raw,omitempty" jsonschema:"base64-encoded binary data (standard base64, RFC 4648). Decode with base64.StdEncoding."`
}

// SendMessageInput is the input schema for the send_message tool.
type SendMessageInput struct {
	Agent              string         `json:"agent" jsonschema:"agent alias from registry or full HTTP/HTTPS URL"`
	Message            string         `json:"message,omitempty" jsonschema:"plain text message to send. Use this for simple text-only messages. Mutually exclusive with 'parts' — if both are provided, 'parts' takes precedence."`
	Parts              []InputPart    `json:"parts,omitempty" jsonschema:"structured message parts for multi-part or non-text content. Use this when sending JSON data, URLs, or mixed content. Takes precedence over 'message' if both are provided."`
	ContextID          string         `json:"context_id,omitempty" jsonschema:"optional context ID to continue an existing conversation"`
	TaskID             string         `json:"task_id,omitempty" jsonschema:"optional task ID to reference an existing task for follow-up messages"`
	Metadata           map[string]any `json:"metadata,omitempty" jsonschema:"optional metadata for A2A protocol extensions (e.g. caller capabilities)"`
	PollTimeoutSeconds *int           `json:"poll_timeout_seconds,omitempty" jsonschema:"max seconds to wait for task completion when polling or streaming (negative = no timeout, default: server configured timeout)"`
}

// BroadcastMessageInput is the input schema for the broadcast_message tool.
type BroadcastMessageInput struct {
	Aliases        []string       `json:"aliases" jsonschema:"list of agent aliases to send the message to (min 1, max 20)"`
	Message        string         `json:"message,omitempty" jsonschema:"plain text message to broadcast. Mutually exclusive with 'parts'."`
	Parts          []InputPart    `json:"parts,omitempty" jsonschema:"structured message parts. Takes precedence over 'message' if both provided."`
	TimeoutSeconds *int           `json:"timeout_seconds,omitempty" jsonschema:"per-agent timeout in seconds (min 1, max 120, default 30)"`
	Metadata       map[string]any `json:"metadata,omitempty" jsonschema:"optional metadata for A2A protocol extensions"`
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

// Requirements: CAC-4.2, CAC-4.3

// CallerSkill represents a skill on the caller agent card.
type CallerSkill struct {
	Name        string `json:"name" jsonschema:"skill name (required)"`
	Description string `json:"description,omitempty" jsonschema:"strongly recommended — provide one even if you need to infer it from the skill name"`
}

// Requirements: CAC-1.6, CAC-4.4

// CallerCapabilities describes supported A2A capabilities.
type CallerCapabilities struct {
	Streaming         bool `json:"streaming,omitempty" jsonschema:"whether the caller supports streaming"`
	PushNotifications bool `json:"pushNotifications,omitempty" jsonschema:"whether the caller supports push notifications"`
}

// Requirements: CAC-4.1

// CallerCard is the stored representation of the caller agent card.
type CallerCard struct {
	Name         string              `json:"name"`
	Description  string              `json:"description"`
	URL          string              `json:"url,omitempty"`
	Skills       []CallerSkill       `json:"skills,omitempty"`
	Capabilities *CallerCapabilities `json:"capabilities,omitempty"`
}

// Requirements: CAC-1.2, CAC-1.3, CAC-1.4, CAC-1.5, CAC-1.6, CAC-1.7

// CreateCallerCardInput is the input schema for the create_caller_card tool.
type CreateCallerCardInput struct {
	Name         string              `json:"name" jsonschema:"the caller agent's display name (required)"`
	Description  string              `json:"description" jsonschema:"what the caller agent does (required)"`
	URL          string              `json:"url,omitempty" jsonschema:"reachable endpoint for callbacks, if available"`
	Skills       []CallerSkill       `json:"skills,omitempty" jsonschema:"list of skills the caller agent supports"`
	Capabilities *CallerCapabilities `json:"capabilities,omitempty" jsonschema:"supported A2A capabilities (e.g., streaming, pushNotifications)"`
	MetadataKey  string              `json:"metadata_key,omitempty" jsonschema:"metadata attribute name the card will be injected under (default: caller_agent_card)"`
}

// ViewCallerCardInput is the input schema for the view_caller_card tool (empty).
type ViewCallerCardInput struct{}

// RemoveCallerCardInput is the input schema for the remove_caller_card tool (empty).
type RemoveCallerCardInput struct{}

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
		Name: "send_message",
		Description: `Send a message to a connected A2A agent by alias or URL.

Use 'message' for simple plain-text messages. Use 'parts' when you need to send structured data (JSON objects), URLs, or multi-part content. Parts also support base64-encoded binary data via the 'raw' field. If both are provided, 'parts' takes precedence.`,
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
		Name: "broadcast_message",
		Description: `Send the same message to multiple agents simultaneously and collect responses.

Use 'message' for simple plain-text broadcasts. Use 'parts' when you need to send structured data or mixed content to all agents. Parts also support base64-encoded binary data via the 'raw' field.`,
	}, s.handleBroadcastMessage)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "discover_agents",
		Description: "Discover available agents from a remote agent directory service",
	}, s.handleDiscoverAgents)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "ping_agent",
		Description: "Perform a liveness check on a registered agent to verify reachability",
	}, s.handlePingAgent)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name: "create_caller_card",
		Description: `Register a caller agent card that is automatically included on all outbound messages (send_message and broadcast_message).

Calling again replaces the previous card. This enables target agents to discover the caller's capabilities and delegate tasks back without requiring a .well-known/agent.json endpoint.`,
	}, s.handleCreateCallerCard)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "view_caller_card",
		Description: "View the currently registered caller agent card, if any",
	}, s.handleViewCallerCard)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "remove_caller_card",
		Description: "Remove the registered caller agent card so it is no longer injected on outbound messages",
	}, s.handleRemoveCallerCard)

	// Register history tools only when history is enabled (depth > 0).
	// Requirements: 5.1, 5.2, 5.3
	if s.historyEnabled {
		mcp.AddTool(s.mcpServer, &mcp.Tool{
			Name:        "get_history",
			Description: "Retrieve the interaction history for a connected agent. Returns a chronological list of past interactions including sent messages and responses.",
		}, s.handleGetHistory)

		mcp.AddTool(s.mcpServer, &mcp.Tool{
			Name:        "clear_history",
			Description: "Clear all interaction history for a connected agent without disconnecting the agent.",
		}, s.handleClearHistory)
	}
}
