package tool

import (
	"context"
	"fmt"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nisimpson/a2a-gateway-mcp/registry"
)

// --- CheckInboxTool ---

// CheckInboxInput is the input schema for the check_inbox tool.
type CheckInboxInput struct {
	Alias string `json:"alias,omitempty" jsonschema:"filter by agent alias"`
}

// CheckInboxOutput is the structured output for the check_inbox tool.
type CheckInboxOutput struct {
	Entries []InboxSummary `json:"entries"`
}

// InboxSummary is a lightweight summary of an inbox entry.
type InboxSummary struct {
	Alias     string `json:"alias"`
	TaskID    string `json:"task_id,omitempty"`
	ContextID string `json:"context_id,omitempty"`
	State     string `json:"state"`
	Timestamp string `json:"timestamp"`
}

// CheckInboxTool implements the check_inbox MCP tool for non-destructive inbox peek.
type CheckInboxTool struct {
	Inbox Inbox
}

// NewCheckInboxTool creates a new CheckInboxTool from the given environment.
func NewCheckInboxTool(env *Env) *CheckInboxTool {
	return &CheckInboxTool{Inbox: env.Inbox}
}

// Tool returns the MCP tool definition for check_inbox.
func (c *CheckInboxTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name: "check_inbox",
		Description: toolDescription(
			"List inbox entries without consuming them.",
			"Returns lightweight summaries of pending async responses.",
			"Optionally filter by agent alias.",
		),
	}
}

// Handle processes a check_inbox request by peeking at the inbox without removing entries.
func (c *CheckInboxTool) Handle(_ context.Context, _ *mcp.CallToolRequest, input *CheckInboxInput) (*mcp.CallToolResult, *CheckInboxOutput, error) {
	// Requirement: AINB-3.1, AINB-3.2, AINB-3.3, AINB-3.4, AINB-3.5, AINB-3.6
	entries := c.Inbox.Peek(registry.InboxPeekFilter{Alias: input.Alias})

	summaries := make([]InboxSummary, 0, len(entries))
	for _, e := range entries {
		summaries = append(summaries, InboxSummary{
			Alias:     e.Alias,
			TaskID:    e.TaskID,
			ContextID: e.ContextID,
			State:     e.State,
			Timestamp: e.Timestamp.Format(time.RFC3339),
		})
	}

	return nil, &CheckInboxOutput{Entries: summaries}, nil
}

// --- ReadInboxTool ---

// ReadInboxInput is the input schema for the read_inbox tool.
type ReadInboxInput struct {
	Alias  string `json:"alias" jsonschema:"agent alias to read messages from"`
	Length *int   `json:"length,omitempty" jsonschema:"max entries to return (FIFO)"`
	Latest bool   `json:"latest,omitempty" jsonschema:"pop all but return only the most recent"`
}

// ReadInboxOutput is the structured output for the read_inbox tool.
type ReadInboxOutput struct {
	Messages []SendMessageOutput `json:"messages"`
}

// ReadInboxTool implements the read_inbox MCP tool for destructive inbox reads.
type ReadInboxTool struct {
	Inbox Inbox
}

// NewReadInboxTool creates a new ReadInboxTool with the given environment.
func NewReadInboxTool(env *Env) *ReadInboxTool {
	return &ReadInboxTool{Inbox: env.Inbox}
}

// Tool returns the MCP tool metadata for read_inbox.
func (r *ReadInboxTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name: "read_inbox",
		Description: toolDescription(
			"Read and consume inbox messages for a specific agent.",
			"Returns full message payloads and removes entries from the inbox.",
			"Use 'length' to limit entries or 'latest' to get only the most recent.",
		),
		OutputSchema: readInboxOutputSchema(),
	}
}

// Handle processes a read_inbox request by popping entries from the inbox
// and converting them to SendMessageOutput format.
func (r *ReadInboxTool) Handle(_ context.Context, _ *mcp.CallToolRequest, input *ReadInboxInput) (*mcp.CallToolResult, *ReadInboxOutput, error) {
	// Requirement: AINB-4.1, AINB-4.2, AINB-4.3, AINB-4.4, AINB-4.5, AINB-4.6, AINB-4.7, AINB-4.8, AINB-4.9
	if input.Alias == "" {
		return nil, nil, fmt.Errorf("alias is required")
	}

	entries := r.Inbox.Pop(registry.InboxPopOptions{
		Alias:  input.Alias,
		Length: input.Length,
		Latest: input.Latest,
	})

	messages := make([]SendMessageOutput, 0, len(entries))
	for _, e := range entries {
		msg := SendMessageOutput{
			Task:    e.Task,
			Message: e.Message,
		}
		messages = append(messages, msg)
	}

	return nil, &ReadInboxOutput{Messages: messages}, nil
}

// readInboxOutputSchema returns a permissive JSON schema for the read_inbox
// tool output. Because the output contains A2A types (Task, Message) with custom
// JSON marshaling (e.g., Part flattens content fields), auto-generated schemas
// from Go reflection don't match the actual wire format. This explicit schema
// accepts the real output shape.
func readInboxOutputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Title:       "ReadInboxOutput",
		Type:        "object",
		Description: "Contains consumed inbox messages for the requested agent.",
		Properties: map[string]*jsonschema.Schema{
			"messages": {
				Type:        "array",
				Description: "Array of message payloads, each containing either a task or message field.",
				Items: &jsonschema.Schema{
					Type: "object",
				},
			},
		},
		Required: []string{"messages"},
	}
}
