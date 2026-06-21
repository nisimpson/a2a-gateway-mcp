package gateway

import (
	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/jsonschema-go/jsonschema"

	"github.com/nisimpson/a2a-gateway-mcp/internal/specification"
)

// output_schemas.go defines output schema types and JSON schema literals
// for all gateway tools. These are used to advertise structuredContent shapes
// via the MCP protocol's OutputSchema field.
//
// Two strategies are used:
// 1. Embedded JSON schemas from internal/specification for send_message and
//    broadcast_message (derived from the A2A specification).
// 2. Output structs with jsonschema: tags for all other tools, letting the
//    MCP SDK reflect on them to auto-populate OutputSchema.
//
// Requirements: SRES-1.2, SRES-6.1

// SendMessageResponse wraps the A2A response in a typed envelope.
// Exactly one of Message or Task will be non-nil.
type SendMessageResponse struct {
	Message *a2a.Message `json:"message,omitempty"`
	Task    *a2a.Task    `json:"task,omitempty"`
}

// sendMessageOutputSchema returns the JSON schema for send_message's structuredContent.
// It uses the embedded A2A specification schemas from internal/specification.
func sendMessageOutputSchema() *jsonschema.Schema {
	schema, err := specification.SendMessageResponseSchema()
	if err != nil {
		panic("sendMessageOutputSchema: " + err.Error())
	}
	return &schema
}

// broadcastMessageOutputSchema returns the JSON schema for broadcast_message's structuredContent.
// It describes a map of agent aliases to SendMessageResponse objects.
func broadcastMessageOutputSchema() *jsonschema.Schema {
	sendSchema, err := specification.SendMessageResponseSchema()
	if err != nil {
		panic("broadcastMessageOutputSchema: " + err.Error())
	}
	// The broadcast response is a map of alias → SendMessageResponse.
	// We reuse the same definitions and reference the send schema's oneOf structure.
	return &jsonschema.Schema{
		Title:                "BroadcastMessageResponse",
		Type:                 "object",
		Description:          "map of agent aliases to per-agent A2A SendMessage responses",
		Definitions:          sendSchema.Definitions,
		AdditionalProperties: &jsonschema.Schema{OneOf: sendSchema.OneOf},
	}
}

// --- Output structs for Strategy B tools (reflected via jsonschema: tags) ---

// ConnectAgentOutput is the output schema for the connect_agent tool.
type ConnectAgentOutput struct {
	Message string `json:"message" jsonschema:"confirmation message indicating successful connection"`
}

// DisconnectAgentOutput is the output schema for the disconnect_agent tool.
type DisconnectAgentOutput struct {
	Message string `json:"message" jsonschema:"confirmation message indicating successful disconnection"`
}

// ListAgentsOutput is the output schema for the list_agents tool.
type ListAgentsOutput struct {
	Agents []ListAgentEntry `json:"agents" jsonschema:"list of registered agents with health and rate limit info"`
}

// ListAgentEntry describes a single agent in the list_agents output.
type ListAgentEntry struct {
	Alias               string `json:"alias" jsonschema:"agent alias"`
	URL                 string `json:"url" jsonschema:"agent URL"`
	RateLimit           string `json:"rate_limit" jsonschema:"rate limit description (e.g. '1.00 rps, burst 5' or 'unlimited')"`
	Health              string `json:"health" jsonschema:"health status (healthy, unhealthy, unknown)"`
	ConsecutiveFailures *int   `json:"consecutive_failures,omitempty" jsonschema:"failure count (only present when unhealthy)"`
}

// PingAgentOutput is the output schema for the ping_agent tool.
type PingAgentOutput struct {
	Reachable    bool   `json:"reachable" jsonschema:"whether the agent responded to the ping"`
	Health       string `json:"health" jsonschema:"health status after ping (healthy, unhealthy, unknown)"`
	ResponseTime *int   `json:"response_time_ms,omitempty" jsonschema:"response time in milliseconds (only present when reachable)"`
}

// GetAgentCardOutput is the output schema for the get_agent_card tool.
type GetAgentCardOutput struct {
	Name         string   `json:"name" jsonschema:"agent display name"`
	Description  string   `json:"description,omitempty" jsonschema:"agent description"`
	URL          string   `json:"url,omitempty" jsonschema:"agent URL"`
	Version      string   `json:"version,omitempty" jsonschema:"agent version"`
	Skills       []any    `json:"skills,omitempty" jsonschema:"agent skills"`
	Capabilities any      `json:"capabilities,omitempty" jsonschema:"agent capabilities object"`
	Provider     any      `json:"provider,omitempty" jsonschema:"agent provider information"`
	InputModes   []string `json:"inputModes,omitempty" jsonschema:"supported input MIME types"`
	OutputModes  []string `json:"outputModes,omitempty" jsonschema:"supported output MIME types"`
}

// GetTaskOutput is the output schema for the get_task tool.
type GetTaskOutput struct {
	ID        string `json:"id" jsonschema:"task identifier"`
	ContextID string `json:"contextId,omitempty" jsonschema:"conversation context identifier"`
	State     string `json:"state" jsonschema:"current task state (completed, working, input-required, failed, canceled)"`
	Response  string `json:"response,omitempty" jsonschema:"task response text extracted from artifacts or status message"`
}

// CancelTaskOutput is the output schema for the cancel_task tool.
type CancelTaskOutput struct {
	Message string `json:"message" jsonschema:"confirmation that the task was canceled"`
}

// GetHistoryOutput is the output schema for the get_history tool.
type GetHistoryOutput struct {
	Entries []HistoryOutputEntry `json:"entries" jsonschema:"chronological list of interaction history entries"`
}

// HistoryOutputEntry describes a single history entry in get_history output.
type HistoryOutputEntry struct {
	Timestamp   string `json:"timestamp" jsonschema:"ISO 8601 timestamp of the interaction"`
	SentMessage string `json:"sent_message" jsonschema:"message sent to the agent"`
	Response    string `json:"response" jsonschema:"response received from the agent"`
	ContextID   string `json:"context_id,omitempty" jsonschema:"context identifier if present"`
	TaskID      string `json:"task_id,omitempty" jsonschema:"task identifier if present"`
	IsError     bool   `json:"is_error,omitempty" jsonschema:"whether the interaction resulted in an error"`
}

// ClearHistoryOutput is the output schema for the clear_history tool.
type ClearHistoryOutput struct {
	Message string `json:"message" jsonschema:"confirmation message indicating history was cleared"`
}

// CreateCallerCardOutput is the output schema for the create_caller_card tool.
type CreateCallerCardOutput struct {
	Message string `json:"message" jsonschema:"confirmation that the caller card was registered"`
}

// ViewCallerCardOutput is the output schema for the view_caller_card tool.
type ViewCallerCardOutput struct {
	Name         string              `json:"name" jsonschema:"caller agent display name"`
	Description  string              `json:"description" jsonschema:"what the caller agent does"`
	URL          string              `json:"url,omitempty" jsonschema:"reachable endpoint for callbacks"`
	Skills       []CallerSkill       `json:"skills,omitempty" jsonschema:"skills the caller agent supports"`
	Capabilities *CallerCapabilities `json:"capabilities,omitempty" jsonschema:"supported A2A capabilities"`
}

// RemoveCallerCardOutput is the output schema for the remove_caller_card tool.
type RemoveCallerCardOutput struct {
	Message string `json:"message" jsonschema:"confirmation that the caller card was removed"`
}

// DiscoverAgentsOutput is the output schema for the discover_agents tool.
type DiscoverAgentsOutput struct {
	Agents []DiscoverAgentEntry `json:"agents" jsonschema:"list of discovered agent cards from the directory"`
}

// DiscoverAgentEntry describes a single agent card returned by discover_agents.
type DiscoverAgentEntry struct {
	Name        string   `json:"name" jsonschema:"agent display name"`
	Description string   `json:"description,omitempty" jsonschema:"agent description"`
	URL         string   `json:"url,omitempty" jsonschema:"agent URL"`
	Version     string   `json:"version,omitempty" jsonschema:"agent version"`
	Skills      []any    `json:"skills,omitempty" jsonschema:"agent skills"`
	InputModes  []string `json:"inputModes,omitempty" jsonschema:"supported input MIME types"`
	OutputModes []string `json:"outputModes,omitempty" jsonschema:"supported output MIME types"`
}
