package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CallerSkill represents a skill on the caller agent card.
type CallerSkill struct {
	Name        string `json:"name" jsonschema:"skill name (required)"`
	Description string `json:"description,omitempty" jsonschema:"strongly recommended — provide one even if you need to infer it from the skill name"`
}

// CallerCapabilities describes supported A2A capabilities.
type CallerCapabilities struct {
	Streaming         bool `json:"streaming,omitempty" jsonschema:"whether the caller supports streaming"`
	PushNotifications bool `json:"pushNotifications,omitempty" jsonschema:"whether the caller supports push notifications"`
}

// CallerCard is the stored representation of the caller agent card.
type CallerCard struct {
	Name         string              `json:"name"`
	Description  string              `json:"description"`
	URL          string              `json:"url,omitempty"`
	Skills       []CallerSkill       `json:"skills,omitempty"`
	Capabilities *CallerCapabilities `json:"capabilities,omitempty"`
}

// --- CreateCallerCardTool ---

// CreateCallerCardInput is the input schema for the create_caller_card tool.
type CreateCallerCardInput struct {
	Name         string              `json:"name" jsonschema:"the caller agent's display name (required)"`
	Description  string              `json:"description" jsonschema:"what the caller agent does (required)"`
	URL          string              `json:"url,omitempty" jsonschema:"reachable endpoint for callbacks, if available"`
	Skills       []CallerSkill       `json:"skills,omitempty" jsonschema:"list of skills the caller agent supports"`
	Capabilities *CallerCapabilities `json:"capabilities,omitempty" jsonschema:"supported A2A capabilities (e.g., streaming, pushNotifications)"`
	MetadataKey  string              `json:"metadata_key,omitempty" jsonschema:"metadata attribute name the card will be injected under (default: caller_agent_card)"`
}

// CreateCallerCardOutput is the output schema for the create_caller_card tool.
type CreateCallerCardOutput struct {
	Message string `json:"message" jsonschema:"confirmation that the caller card was registered"`
}

// CreateCallerCardTool registers or replaces the caller agent card.
type CreateCallerCardTool struct {
	Store CallerCardStore
}

// NewCreateCallerCardTool creates a new CreateCallerCardTool using the caller card store from the given environment.
func NewCreateCallerCardTool(env *Env) *CreateCallerCardTool {
	return &CreateCallerCardTool{
		Store: env.CallerCardStore,
	}
}

func (c *CreateCallerCardTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name: "create_caller_card",
		Description: toolDescription(
			"Register a caller agent card that is automatically included on all outbound messages",
			"(send_message and broadcast_message). Calling again replaces the previous card.",
			"This enables target agents to discover the caller's capabilities and delegate tasks back",
			"without requiring a .well-known/agent-card.json endpoint.",
		),
	}
}

func (c *CreateCallerCardTool) Handle(_ context.Context, _ *mcp.CallToolRequest, input *CreateCallerCardInput) (*mcp.CallToolResult, *CreateCallerCardOutput, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, nil, errors.New("name must not be empty or whitespace-only")
	}

	card := &CallerCard{
		Name:         input.Name,
		Description:  input.Description,
		URL:          input.URL,
		Skills:       input.Skills,
		Capabilities: input.Capabilities,
	}

	c.Store.Set(card, input.MetadataKey)

	output := &CreateCallerCardOutput{
		Message: fmt.Sprintf("Caller agent card registered for %q", input.Name),
	}
	return nil, output, nil
}

// --- ViewCallerCardTool ---

// ViewCallerCardInput is the input schema for the view_caller_card tool (empty).
type ViewCallerCardInput struct{}

// ViewCallerCardTool returns the currently stored caller agent card as JSON.
type ViewCallerCardTool struct {
	Store CallerCardStore
}

// NewViewCallerCardTool creates a new ViewCallerCardTool using the caller card store from the given environment.
func NewViewCallerCardTool(env *Env) *ViewCallerCardTool {
	return &ViewCallerCardTool{
		Store: env.CallerCardStore,
	}
}

func (v *ViewCallerCardTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "view_caller_card",
		Description: "View the currently registered caller agent card, if any",
	}
}

func (v *ViewCallerCardTool) Handle(_ context.Context,
	_ *mcp.CallToolRequest, _ *ViewCallerCardInput) (*mcp.CallToolResult, any, error) {
	card := v.Store.Get()
	if card == nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "no caller agent card is currently set"}},
		}, nil, nil
	}

	data, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to serialize caller card: %v", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

// RemoveCallerCardInput is the input schema for the remove_caller_card tool (empty).
type RemoveCallerCardInput struct{}

// RemoveCallerCardOutput is the output schema for the remove_caller_card tool.
type RemoveCallerCardOutput struct {
	Message string `json:"message" jsonschema:"confirmation that the caller card was removed"`
}

// RemoveCallerCardTool clears the stored caller agent card.
type RemoveCallerCardTool struct {
	Store CallerCardStore
}

// NewRemoveCallerCardTool creates a new NewRemoveCallerCardTool using the caller card store from the given environment.
func NewRemoveCallerCardTool(env *Env) *RemoveCallerCardTool {
	return &RemoveCallerCardTool{
		Store: env.CallerCardStore,
	}
}

func (r *RemoveCallerCardTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "remove_caller_card",
		Description: "Remove the registered caller agent card so it is no longer injected on outbound messages",
	}
}

func (r *RemoveCallerCardTool) Handle(_ context.Context, _ *mcp.CallToolRequest, _ *RemoveCallerCardInput) (*mcp.CallToolResult, *RemoveCallerCardOutput, error) {
	had := r.Store.Remove()
	if !had {
		return nil, nil, errors.New("no caller agent card was set")
	}

	output := &RemoveCallerCardOutput{
		Message: "Caller agent card removed",
	}
	return nil, output, nil
}
