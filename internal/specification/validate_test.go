package specification

import (
	"encoding/json"
	"testing"
)

func TestValidateExampleAgainstSendMessageResponseSchema(t *testing.T) {
	schema, err := SendMessageResponseSchema()
	if err != nil {
		t.Fatalf("failed to build schema: %v", err)
	}

	schemaBytes, err := json.MarshalIndent(&schema, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal schema: %v", err)
	}
	t.Logf("Schema JSON:\n%s", string(schemaBytes))

	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("failed to resolve schema: %v", err)
	}

	// Example response: a message response
	messageResponse := map[string]any{
		"message": map[string]any{
			"messageId": "019ee82c-700a-705d-835a-e2cf7a360831",
			"contextId": "ctx-tool-1",
			"parts": []any{
				map[string]any{"text": "agent reply"},
			},
			"role": "ROLE_AGENT",
		},
	}

	if err := resolved.Validate(&messageResponse); err != nil {
		t.Errorf("message response failed validation:\n%v", err)
	} else {
		t.Log("message response: PASSED")
	}

	// Example response: a task response
	taskResponse := map[string]any{
		"task": map[string]any{
			"id":        "task-123",
			"status":    map[string]any{"state": "TASK_STATE_COMPLETED"},
			"contextId": "ctx-456",
			"artifacts": []any{
				map[string]any{
					"artifactId": "art-1",
					"parts":      []any{map[string]any{"text": "hello world"}},
				},
			},
		},
	}

	if err := resolved.Validate(&taskResponse); err != nil {
		t.Errorf("task response failed validation:\n%v", err)
	} else {
		t.Log("task response: PASSED")
	}
}
