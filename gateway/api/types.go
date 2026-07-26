package api

import (
	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/nisimpson/a2a-gateway-mcp/history"
	"github.com/nisimpson/a2a-gateway-mcp/registry"
)

// CallerSkill is a skill on the caller agent card.
// This is an alias for [registry.CallerSkill] to avoid type incompatibility.
type CallerSkill = registry.CallerSkill

// CallerCapabilities describes the A2A capabilities the caller supports.
// This is an alias for [registry.CallerCapabilities] to avoid type incompatibility.
type CallerCapabilities = registry.CallerCapabilities

// InputPart represents a single content part in a multi-part message.
// Exactly one of Text, Data, URL, or Raw should be set.
type InputPart struct {
	// Text contains plain text content.
	Text *string `json:"text,omitempty" jsonschema:"plain text content"`
	// Data contains structured JSON data to send as a DataPart.
	Data any `json:"data,omitempty" jsonschema:"structured JSON data (object, array, or primitive)"`
	// URL contains a URL reference to send as a URLPart.
	URL *string `json:"url,omitempty" jsonschema:"URL reference"`
	// Raw contains base64-encoded binary data (standard base64, RFC 4648).
	Raw *string `json:"raw,omitempty" jsonschema:"base64-encoded binary data (standard base64, RFC 4648). Decode with base64.StdEncoding."`
}

// --- ConnectAgent ---

// ConnectAgentInput contains the parameters for registering a remote A2A agent.
type ConnectAgentInput struct {
	// Alias is a short identifier for the agent (lowercase alphanumeric and hyphens, max 64 chars).
	Alias string `json:"alias" jsonschema:"short alias for the agent (lowercase alphanumeric and hyphens only, max 64 chars)"`
	// AgentURL is the HTTP or HTTPS URL of the A2A agent.
	AgentURL string `json:"agent_url" jsonschema:"HTTP or HTTPS URL of the A2A agent"`
	// Headers are optional HTTP headers included on all requests to this agent.
	Headers map[string]string `json:"headers,omitempty" jsonschema:"optional HTTP headers to include on all requests to this agent (max 20 entries)"`
	// RateLimitRPS sets the per-agent rate limit in requests per second.
	// Must be provided together with RateLimitBurst.
	RateLimitRPS *float64 `json:"rate_limit_rps,omitempty" jsonschema:"requests per second rate limit for this agent (must be provided with rate_limit_burst)"`
	// RateLimitBurst sets the burst capacity for the agent's rate limiter.
	// Must be provided together with RateLimitRPS.
	RateLimitBurst *int `json:"rate_limit_burst,omitempty" jsonschema:"burst capacity for this agent's rate limiter (must be provided with rate_limit_rps)"`
	// PingEndpoint is an optional relative URL path for liveness checks.
	PingEndpoint *string `json:"ping_endpoint,omitempty" jsonschema:"relative URL path for liveness checks (starts with /, max 256 chars)"`
}

// ConnectAgentOutput is the result of a successful agent connection.
type ConnectAgentOutput struct {
	// Message is a confirmation message indicating successful connection.
	Message string `json:"message" jsonschema:"confirmation message indicating successful connection"`
}

// --- DisconnectAgent ---

// DisconnectAgentInput contains the parameters for removing a registered agent.
type DisconnectAgentInput struct {
	// Alias is the alias of the agent to disconnect.
	Alias string `json:"alias" jsonschema:"alias of the agent to disconnect"`
}

// DisconnectAgentOutput is the result of a successful agent disconnection.
type DisconnectAgentOutput struct {
	// Message is a confirmation message indicating successful disconnection.
	Message string `json:"message" jsonschema:"confirmation message indicating successful disconnection"`
}

// --- SendMessage ---

// SendMessageInput contains the parameters for sending a message to an A2A agent.
type SendMessageInput struct {
	// Agent is the agent alias from registry or a full HTTP/HTTPS URL.
	Agent string `json:"agent" jsonschema:"agent alias from registry or full HTTP/HTTPS URL"`
	// Message is a plain text message to send. Mutually exclusive with Parts.
	Message string `json:"message,omitempty" jsonschema:"plain text message to send. Use this for simple text-only messages. Mutually exclusive with 'parts' — if both are provided, 'parts' takes precedence."`
	// Parts are structured message parts for multi-part or non-text content.
	// Takes precedence over Message if both are provided.
	Parts []InputPart `json:"parts,omitempty" jsonschema:"structured message parts for multi-part or non-text content. Use this when sending JSON data, URLs, or mixed content. Takes precedence over 'message' if both are provided."`
	// ContextID is an optional context ID to continue an existing conversation.
	ContextID string `json:"context_id,omitempty" jsonschema:"optional context ID to continue an existing conversation"`
	// TaskID is an optional task ID to reference an existing task for follow-up messages.
	TaskID string `json:"task_id,omitempty" jsonschema:"optional task ID to reference an existing task for follow-up messages"`
	// Metadata contains optional metadata for A2A protocol extensions.
	Metadata map[string]any `json:"metadata,omitempty" jsonschema:"optional metadata for A2A protocol extensions (e.g. caller capabilities)"`
	// PollTimeoutSeconds is the max seconds to wait for task completion when
	// polling or streaming. Negative means no timeout.
	PollTimeoutSeconds *int `json:"poll_timeout_seconds,omitempty" jsonschema:"max seconds to wait for task completion when polling or streaming (negative = no timeout, default: server configured timeout)"`
	// Async, if true, returns immediately and deposits response in the inbox.
	Async *bool `json:"async,omitempty" jsonschema:"if true, return immediately and deposit response in inbox"`
}

// SendMessageOutput wraps the A2A response in a typed envelope.
// Exactly one of Message, Task, or Async will be non-nil.
type SendMessageOutput struct {
	// Message is the agent's direct message response.
	Message *a2a.Message `json:"message,omitempty"`
	// Task is the agent's task response (for long-running operations).
	Task *a2a.Task `json:"task,omitempty"`
	// Async is the dispatch confirmation when Async mode is used.
	Async *AsyncSendOutput `json:"async,omitempty"`
}

// AsyncSendOutput is returned when Async is true. It confirms the message
// was dispatched without waiting for the agent's response.
type AsyncSendOutput struct {
	// Alias is the agent alias the message was dispatched to.
	Alias string `json:"alias" jsonschema:"agent alias the message was dispatched to"`
	// Status is the dispatch status (dispatched or error).
	Status string `json:"status" jsonschema:"dispatch status (dispatched or error)"`
	// Error is the error message if dispatch failed immediately.
	Error string `json:"error,omitempty" jsonschema:"error message if dispatch failed immediately"`
}

// --- BroadcastMessage ---

// BroadcastMessageInput contains the parameters for broadcasting a message
// to multiple agents simultaneously.
type BroadcastMessageInput struct {
	// Aliases is the list of agent aliases to send the message to (min 1, max 20).
	Aliases []string `json:"aliases" jsonschema:"list of agent aliases to send the message to (min 1, max 20)"`
	// Message is a plain text message to broadcast. Mutually exclusive with Parts.
	Message string `json:"message,omitempty" jsonschema:"plain text message to broadcast. Mutually exclusive with 'parts'."`
	// Parts are structured message parts. Takes precedence over Message if both provided.
	Parts []InputPart `json:"parts,omitempty" jsonschema:"structured message parts. Takes precedence over 'message' if both provided."`
	// TimeoutSeconds is the per-agent timeout in seconds (min 1, max 120, default 30).
	TimeoutSeconds *int `json:"timeout_seconds,omitempty" jsonschema:"per-agent timeout in seconds (min 1, max 120, default 30)"`
	// Metadata contains optional metadata for A2A protocol extensions.
	Metadata map[string]any `json:"metadata,omitempty" jsonschema:"optional metadata for A2A protocol extensions"`
	// Async, if true, returns immediately and deposits responses in the inbox.
	Async *bool `json:"async,omitempty" jsonschema:"if true, return immediately and deposit responses in inbox"`
}

// --- GetTask ---

// GetTaskInput contains the parameters for retrieving task state.
type GetTaskInput struct {
	// Agent is the agent alias from registry or a full HTTP/HTTPS URL.
	Agent string `json:"agent" jsonschema:"agent alias from registry or full HTTP/HTTPS URL"`
	// TaskID is the task identifier to retrieve.
	TaskID string `json:"task_id" jsonschema:"the task identifier to retrieve"`
}

// --- CancelTask ---

// CancelTaskInput contains the parameters for canceling a task.
type CancelTaskInput struct {
	// Agent is the agent alias from registry or a full HTTP/HTTPS URL.
	Agent string `json:"agent" jsonschema:"agent alias from registry or full HTTP/HTTPS URL"`
	// TaskID is the task identifier to cancel.
	TaskID string `json:"task_id" jsonschema:"the task identifier to cancel"`
}

// CancelTaskOutput is the result of a successful task cancellation.
type CancelTaskOutput struct {
	// Message is a confirmation that the task was canceled.
	Message string `json:"message" jsonschema:"confirmation that the task was canceled"`
}

// --- DiscoverAgents ---

// DiscoverAgentsInput contains the parameters for discovering agents from a directory.
type DiscoverAgentsInput struct {
	// DirectoryURL is the HTTP or HTTPS URL of the agent directory service.
	// Optional if the server has a default directory configured.
	DirectoryURL string `json:"directory_url" jsonschema:"HTTP or HTTPS URL of the agent directory service"`
	// Filter is a free-text search filter passed to the directory (max 256 chars).
	Filter string `json:"filter,omitempty" jsonschema:"free-text search filter passed to the directory (max 256 chars)"`
	// Limit is the maximum number of agent cards to return (min 1).
	Limit *int `json:"limit,omitempty" jsonschema:"maximum number of agent cards to return (min 1)"`
	// Cursor is a cursor token for pagination.
	Cursor string `json:"cursor,omitempty" jsonschema:"cursor token for pagination, pass the next_token from previous response to get the next page of results"`
	// Headers are optional HTTP headers for directory authentication.
	Headers map[string]string `json:"headers,omitempty" jsonschema:"optional HTTP headers for directory authentication (max 20 entries)"`
	// Help, if true, returns filter help documentation instead of agent cards.
	Help bool `json:"help,omitempty" jsonschema:"if true, return filter help documentation instead of agent cards"`
}

// DiscoverAgentsOutput is the result of a directory discovery query.
type DiscoverAgentsOutput struct {
	// Agents is the list of discovered agent cards from the directory.
	Agents []a2a.AgentCard `json:"agents" jsonschema:"list of discovered agent cards from the directory"`
	// NextToken is a cursor token for the next page of results.
	NextToken string `json:"next_token,omitempty" jsonschema:"cursor token for next page of results"`
}

// --- GetAgentCard ---

// GetAgentCardInput contains the parameters for retrieving an agent's card.
type GetAgentCardInput struct {
	// Agent is the agent alias from registry or a full HTTP/HTTPS URL.
	Agent string `json:"agent" jsonschema:"agent alias from registry or full HTTP/HTTPS URL"`
}

// GetAgentCardOutput contains the parsed agent card information.
type GetAgentCardOutput struct {
	// Name is the agent's display name.
	Name string `json:"name" jsonschema:"agent display name"`
	// Description is the agent's description.
	Description string `json:"description,omitempty" jsonschema:"agent description"`
	// URL is the agent's URL.
	URL string `json:"url,omitempty" jsonschema:"agent URL"`
	// Version is the agent version.
	Version string `json:"version,omitempty" jsonschema:"agent version"`
	// Skills are the agent's declared skills.
	Skills []any `json:"skills,omitempty" jsonschema:"agent skills"`
	// Capabilities is the agent's capabilities object.
	Capabilities any `json:"capabilities,omitempty" jsonschema:"agent capabilities object"`
	// Provider is the agent provider information.
	Provider any `json:"provider,omitempty" jsonschema:"agent provider information"`
	// InputModes lists supported input MIME types.
	InputModes []string `json:"inputModes,omitempty" jsonschema:"supported input MIME types"`
	// OutputModes lists supported output MIME types.
	OutputModes []string `json:"outputModes,omitempty" jsonschema:"supported output MIME types"`
}

// --- PingAgent ---

// PingAgentInput contains the parameters for performing a liveness check on an agent.
type PingAgentInput struct {
	// Alias is the alias of the agent to ping.
	Alias string `json:"alias" jsonschema:"alias of the agent to ping (lowercase alphanumeric and hyphens, max 64 chars)"`
}

// PingAgentOutput is the result of a ping operation.
type PingAgentOutput struct {
	// Reachable is whether the agent responded to the ping.
	Reachable bool `json:"reachable" jsonschema:"whether the agent responded to the ping"`
	// Health is the health status after ping (healthy, unhealthy, unknown).
	Health string `json:"health" jsonschema:"health status after ping (healthy, unhealthy, unknown)"`
	// ResponseTime is the round-trip time in milliseconds (only present when reachable).
	ResponseTime *int `json:"response_time_ms,omitempty" jsonschema:"response time in milliseconds (only present when reachable)"`
}

// --- ListAgents ---

// ListAgentsInput is the input for listing all connected agents (empty).
type ListAgentsInput struct{}

// ListAgentsOutput is the result of listing connected agents.
type ListAgentsOutput struct {
	// Agents is the list of registered agents with health and rate limit info.
	Agents []ListAgentEntry `json:"agents" jsonschema:"list of registered agents with health and rate limit info"`
}

// ListAgentEntry describes a single agent in the list_agents output.
type ListAgentEntry struct {
	// Alias is the agent's registered alias.
	Alias string `json:"alias" jsonschema:"agent alias"`
	// URL is the agent's endpoint URL.
	URL string `json:"url" jsonschema:"agent URL"`
	// RateLimit describes the rate limit (e.g. "1.00 rps, burst 5" or "unlimited").
	RateLimit string `json:"rate_limit" jsonschema:"rate limit description (e.g. '1.00 rps, burst 5' or 'unlimited')"`
	// Health is the health status (healthy, unhealthy, unknown).
	Health string `json:"health" jsonschema:"health status (healthy, unhealthy, unknown)"`
	// ConsecutiveFailures is the failure count (only present when unhealthy).
	ConsecutiveFailures *int `json:"consecutive_failures,omitempty" jsonschema:"failure count (only present when unhealthy)"`
}

// --- GetHistory ---

// GetHistoryInput contains the parameters for retrieving agent interaction history.
type GetHistoryInput struct {
	// Agent is the agent alias to retrieve history for.
	Agent string `json:"agent" jsonschema:"agent alias to retrieve history for"`
	// Limit is the maximum number of recent entries to return.
	Limit *int `json:"limit,omitempty" jsonschema:"maximum number of recent entries to return"`
}

// GetHistoryOutput is the result of retrieving agent interaction history.
type GetHistoryOutput struct {
	// Entries is the chronological list of interaction history entries.
	Entries []history.Entry `json:"entries" jsonschema:"chronological list of interaction history entries"`
}

// --- ClearHistory ---

// ClearHistoryInput contains the parameters for clearing agent interaction history.
type ClearHistoryInput struct {
	// Agent is the agent alias to clear history for.
	Agent string `json:"agent" jsonschema:"agent alias to clear history for"`
}

// ClearHistoryOutput is the result of clearing history.
type ClearHistoryOutput struct {
	// Message is a confirmation that history was cleared.
	Message string `json:"message" jsonschema:"confirmation message indicating history was cleared"`
}

// --- CreateCallerCard ---

// CreateCallerCardInput contains the parameters for registering a caller agent card.
type CreateCallerCardInput struct {
	// Name is the caller agent's display name (required).
	Name string `json:"name" jsonschema:"the caller agent's display name (required)"`
	// Description is what the caller agent does (required).
	Description string `json:"description" jsonschema:"what the caller agent does (required)"`
	// URL is a reachable endpoint for callbacks, if available.
	URL string `json:"url,omitempty" jsonschema:"reachable endpoint for callbacks, if available"`
	// Skills is a list of skills the caller agent supports.
	Skills []CallerSkill `json:"skills,omitempty" jsonschema:"list of skills the caller agent supports"`
	// Capabilities are supported A2A capabilities.
	Capabilities *CallerCapabilities `json:"capabilities,omitempty" jsonschema:"supported A2A capabilities (e.g., streaming, pushNotifications)"`
	// MetadataKey is the metadata attribute name the card will be injected under.
	MetadataKey string `json:"metadata_key,omitempty" jsonschema:"metadata attribute name the card will be injected under (default: caller_agent_card)"`
}

// CreateCallerCardOutput is the result of creating a caller card.
type CreateCallerCardOutput struct {
	// Message is a confirmation that the caller card was registered.
	Message string `json:"message" jsonschema:"confirmation that the caller card was registered"`
}

// --- ViewCallerCard ---

// ViewCallerCardInput is the input for viewing the current caller card (empty).
type ViewCallerCardInput struct{}

// --- RemoveCallerCard ---

// RemoveCallerCardInput is the input for removing a caller card (empty).
type RemoveCallerCardInput struct{}

// RemoveCallerCardOutput is the result of removing a caller card.
type RemoveCallerCardOutput struct {
	// Message is a confirmation that the caller card was removed.
	Message string `json:"message" jsonschema:"confirmation that the caller card was removed"`
}

// --- CheckInbox ---

// CheckInboxInput contains the parameters for checking the inbox.
type CheckInboxInput struct {
	// Alias filters entries by agent alias. Empty means all agents.
	Alias string `json:"alias,omitempty" jsonschema:"filter by agent alias"`
}

// CheckInboxOutput is the result of checking the inbox.
type CheckInboxOutput struct {
	// Entries are lightweight summaries of pending async responses.
	Entries []InboxSummary `json:"entries"`
}

// InboxSummary is a lightweight summary of an inbox entry.
type InboxSummary struct {
	// Alias is the agent alias.
	Alias string `json:"alias"`
	// TaskID is the task identifier, if applicable.
	TaskID string `json:"task_id,omitempty"`
	// ContextID is the conversation context identifier.
	ContextID string `json:"context_id,omitempty"`
	// State is the entry state (completed, working, error, etc.).
	State string `json:"state"`
	// Timestamp is when the entry was deposited.
	Timestamp string `json:"timestamp"`
}

// --- ReadInbox ---

// ReadInboxInput contains the parameters for reading and consuming inbox entries.
type ReadInboxInput struct {
	// Alias is the agent alias to read messages from (required).
	Alias string `json:"alias" jsonschema:"agent alias to read messages from"`
	// Length limits the max entries to return (FIFO).
	Length *int `json:"length,omitempty" jsonschema:"max entries to return (FIFO)"`
	// Latest pops all but returns only the most recent entry.
	Latest bool `json:"latest,omitempty" jsonschema:"pop all but return only the most recent"`
}

// ReadInboxOutput is the result of reading inbox entries.
type ReadInboxOutput struct {
	// Messages are the consumed inbox messages in SendMessageOutput format.
	Messages []SendMessageOutput `json:"messages"`
}
