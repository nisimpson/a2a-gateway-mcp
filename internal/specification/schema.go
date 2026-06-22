package specification

import (
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/google/jsonschema-go/jsonschema"
)

var (
	//go:embed schema/a2a_message.json
	a2aMessageJSON []byte
	//go:embed schema/a2a_task.json
	a2aTaskJSON []byte
)

// MessageSchema loads and returns the embedded A2A message JSON schema.
func MessageSchema() (jsonschema.Schema, error) {
	schema := jsonschema.Schema{}
	err := json.Unmarshal(a2aMessageJSON, &schema)
	return schema, err
}

// TaskSchema loads and returns the embedded A2A task JSON schema.
func TaskSchema() (jsonschema.Schema, error) {
	schema := jsonschema.Schema{}
	err := json.Unmarshal(a2aTaskJSON, &schema)
	return schema, err
}

// SendMessageResponseSchema constructs a JSON Schema representing the A2A SendMessage response,
// which is a oneOf between a Message and a Task payload.
// The Message and Task schemas are inlined directly in the oneOf properties
// for compatibility with JSON schema validators that may not resolve $ref within oneOf.
func SendMessageResponseSchema() (jsonschema.Schema, error) {
	message, messageErr := MessageSchema()
	task, taskErr := TaskSchema()

	// Hoist nested definitions to the top level so $ref pointers resolve correctly.
	topDefs := map[string]*jsonschema.Schema{}
	for name, def := range message.Definitions {
		topDefs[name] = def
	}
	for name, def := range task.Definitions {
		topDefs[name] = def
	}
	// Clear nested definitions to avoid duplication.
	message.Definitions = nil
	task.Definitions = nil

	schema := jsonschema.Schema{
		Title:       "SendMessageResponse",
		Type:        "object",
		Description: "The target payload mapping exactly to the A2A spec 'oneof' behavior.",
		OneOf: []*jsonschema.Schema{
			{
				Properties: map[string]*jsonschema.Schema{
					"message": &message,
				},
				Required:    []string{"message"},
				Description: "Returned when the interaction is short-lived or synchronous.",
			},
			{
				Properties: map[string]*jsonschema.Schema{
					"task": &task,
				},
				Required:    []string{"task"},
				Description: "Returned when a long-running workflow or background execution is triggered.",
			},
			{
				Properties: map[string]*jsonschema.Schema{
					"async": {
						Type:        "object",
						Description: "Returned when async: true — confirms dispatch without waiting for the agent's response.",
						Properties: map[string]*jsonschema.Schema{
							"alias": {
								Type:        "string",
								Description: "Agent alias the message was dispatched to.",
							},
							"status": {
								Type:        "string",
								Description: "Dispatch status (dispatched or error).",
							},
							"error": {
								Type:        "string",
								Description: "Error message if dispatch failed immediately.",
							},
						},
						Required: []string{"alias", "status"},
					},
				},
				Required:    []string{"async"},
				Description: "Returned when async: true — the message was dispatched without blocking.",
			},
		},
		Definitions: topDefs,
	}

	return schema, errors.Join(messageErr, taskErr)
}
