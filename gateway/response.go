package gateway

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// renderPart converts an a2a.Part to its string representation.
// Returns the rendered string and true if the part was successfully rendered,
// or empty string and false if the part is nil or has an unrecognized content type.
func renderPart(part *a2a.Part) (string, bool) {
	if part == nil {
		return "", false
	}
	switch v := part.Content.(type) {
	case a2a.Text:
		return string(v), true
	case a2a.Data:
		data, err := json.Marshal(v.Value)
		if err != nil {
			return fmt.Sprintf("[unserializable data: %v]", err), true
		}
		return string(data), true
	case a2a.URL:
		return string(v), true
	case a2a.Raw:
		return base64.StdEncoding.EncodeToString([]byte(v)), true
	default:
		return "", false
	}
}

// FormatMessageResponse formats an a2a.Message response as MCP content items.
// The second return value is a *SendMessageResponse wrapping the message for
// use as structured content.
//
// Behavior:
//   - Renders all parts (Text, Data, URL, Raw) using renderPart, concatenated in order.
//   - If the message has zero parts, returns empty text.
//   - Metadata (context_id) is available via the structured *SendMessageResponse;
//     only human-readable response text is included in content.
//
// Content ordering: [response text]
func FormatMessageResponse(msg *a2a.Message) (*mcp.CallToolResult, *SendMessageResponse) {
	// Requirement: SRES-2.1, SRES-2.2 — return raw message as structured content
	result := &mcp.CallToolResult{}

	text := extractContentFromMessageParts(msg.Parts)
	result.Content = append(result.Content, &mcp.TextContent{Text: text})

	return result, &SendMessageResponse{Message: msg}
}

// extractContentFromMessageParts renders all parts using renderPart and
// concatenates results with no separator. Nil parts are skipped.
func extractContentFromMessageParts(parts a2a.ContentParts) string {
	var texts []string

	for _, part := range parts {
		if rendered, ok := renderPart(part); ok {
			texts = append(texts, rendered)
		}
	}

	return strings.Join(texts, "")
}

// FormatInterruptedResponse formats a response for a task in an interrupted
// state (e.g. input-required, auth-required). The second return value is a
// *SendMessageResponse wrapping the task for use as structured content. It includes:
//   - The agent's status message (explaining what input is needed), or
//     artifact content if available.
//   - A "state:<stateName>" indicator so human readers can see the interrupted state.
//
// Metadata (task_id, context_id) is available via the structured *SendMessageResponse;
// only human-readable content is included in the content array.
//
// Content ordering: [response text, state indicator]
func FormatInterruptedResponse(task *a2a.Task, stateName string) (*mcp.CallToolResult, *SendMessageResponse) {
	// Requirement: SRES-3.1, SRES-3.2 — structured content for interrupted states
	result := &mcp.CallToolResult{}

	// Prefer status message text (agents typically explain what they need here).
	var responseText string
	if task.Status.Message != nil {
		responseText = extractStatusMessageText(task.Status.Message)
	}

	// Fall back to artifact content if no status message.
	if responseText == "" {
		responseText = extractContentFromArtifacts(task.Artifacts)
	}

	if responseText != "" {
		result.Content = append(result.Content, &mcp.TextContent{Text: responseText})
	}

	// Include task state so callers can programmatically detect the interrupted state.
	result.Content = append(result.Content, &mcp.TextContent{
		Text: "state:" + stateName,
	})

	return result, &SendMessageResponse{Task: task}
}

// FormatInputRequiredResponse formats a response for a task in the
// input-required state. This is a convenience wrapper around
// FormatInterruptedResponse. The second return value is a *SendMessageResponse
// wrapping the task for use as structured content.
func FormatInputRequiredResponse(task *a2a.Task) (*mcp.CallToolResult, *SendMessageResponse) {
	// Requirement: SRES-3.1 — structured content for input-required state
	return FormatInterruptedResponse(task, "input-required")
}

// FormatTaskResponse extracts content from an A2A Task and formats it
// as MCP CallToolResult content items. The second return value is a
// *SendMessageResponse wrapping the task for use as structured content.
//
// Behavior:
//   - Renders all parts (Text, Data, URL, Raw) from artifacts using renderPart.
//   - Parts within the same artifact are concatenated with no separator.
//   - Content from different artifacts is separated by newline.
//   - If the task has no artifacts or artifacts with no parts, returns empty text.
//   - Metadata (task_id, context_id) is available via the structured *SendMessageResponse;
//     only human-readable response text is included in content.
//
// Content ordering: [response text]
func FormatTaskResponse(task *a2a.Task) (*mcp.CallToolResult, *SendMessageResponse) {
	// Requirement: SRES-1.1, SRES-1.3, SRES-6.3 — return raw task as structured content
	result := &mcp.CallToolResult{}

	text := extractContentFromArtifacts(task.Artifacts)
	result.Content = append(result.Content, &mcp.TextContent{Text: text})

	return result, &SendMessageResponse{Task: task}
}

// extractContentFromArtifacts renders all parts from all artifacts using renderPart.
// Parts within the same artifact are concatenated with no separator.
// Content from different artifacts is separated by a newline character.
func extractContentFromArtifacts(artifacts []*a2a.Artifact) string {
	var artifactTexts []string

	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		var parts []string
		for _, part := range artifact.Parts {
			if rendered, ok := renderPart(part); ok {
				parts = append(parts, rendered)
			}
		}
		if len(parts) > 0 {
			artifactTexts = append(artifactTexts, strings.Join(parts, ""))
		}
	}

	return strings.Join(artifactTexts, "\n")
}
