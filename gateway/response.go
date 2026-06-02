package gateway

import (
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// FormatInputRequiredResponse formats a response for a task in the
// input-required state. It includes:
//   - The agent's status message (explaining what input is needed), or
//     artifact text if available.
//   - A "state:input-required" indicator so callers can programmatically
//     distinguish this from a completed response.
//   - The context_id for follow-up messages.
func FormatInputRequiredResponse(task *a2a.Task) *mcp.CallToolResult {
	result := &mcp.CallToolResult{}

	// Prefer status message text (agents typically explain what they need here).
	var responseText string
	if task.Status.Message != nil {
		responseText = extractStatusMessageText(task.Status.Message)
	}

	// Fall back to artifact text if no status message.
	if responseText == "" {
		text, hasTextParts := extractTextFromArtifacts(task.Artifacts)
		hasAnyParts := artifactsHaveAnyParts(task.Artifacts)
		switch {
		case hasTextParts:
			responseText = text
		case hasAnyParts:
			responseText = "response contained non-text content that cannot be displayed"
		}
	}

	if responseText != "" {
		result.Content = append(result.Content, &mcp.TextContent{Text: responseText})
	}

	// Include task state so callers can programmatically detect input-required.
	result.Content = append(result.Content, &mcp.TextContent{
		Text: "state:input-required",
	})

	if task.ContextID != "" {
		result.Content = append(result.Content, &mcp.TextContent{
			Text: "context_id:" + task.ContextID,
		})
	}

	return result
}

// FormatTaskResponse extracts text content from an A2A Task and formats it
// as MCP CallToolResult content items.
//
// Behavior:
//   - If the task has artifacts with TextParts, concatenates all TextPart text
//     values in artifact order separated by newlines between artifacts.
//   - If the task has artifacts with ONLY non-text parts (no TextParts at all),
//     returns a message indicating non-text content cannot be displayed.
//   - If the task has no artifacts or artifacts with no parts, returns empty text.
//   - If the task has a ContextID, includes it as a separate text content item
//     prefixed with "context_id:".
func FormatTaskResponse(task *a2a.Task) *mcp.CallToolResult {
	result := &mcp.CallToolResult{}

	text, hasTextParts := extractTextFromArtifacts(task.Artifacts)
	hasAnyParts := artifactsHaveAnyParts(task.Artifacts)

	switch {
	case hasTextParts:
		result.Content = append(result.Content, &mcp.TextContent{Text: text})
	case hasAnyParts:
		// Artifacts exist with parts, but none are text parts.
		result.Content = append(result.Content, &mcp.TextContent{
			Text: "response contained non-text content that cannot be displayed",
		})
	default:
		// No artifacts or no parts at all.
		result.Content = append(result.Content, &mcp.TextContent{Text: ""})
	}

	if task.ContextID != "" {
		result.Content = append(result.Content, &mcp.TextContent{
			Text: "context_id:" + task.ContextID,
		})
	}

	return result
}

// extractTextFromArtifacts concatenates TextPart content from all artifacts,
// separated by newlines, in artifact order. It also returns whether any
// TextParts were found (even if their content is empty).
func extractTextFromArtifacts(artifacts []*a2a.Artifact) (string, bool) {
	var artifactTexts []string
	foundTextPart := false

	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		var parts []string
		for _, part := range artifact.Parts {
			if part == nil {
				continue
			}
			if _, ok := part.Content.(a2a.Text); ok {
				foundTextPart = true
				parts = append(parts, part.Text())
			}
		}
		if len(parts) > 0 {
			artifactTexts = append(artifactTexts, strings.Join(parts, ""))
		}
	}

	return strings.Join(artifactTexts, "\n"), foundTextPart
}

// artifactsHaveAnyParts returns true if any artifact contains at least one part
// (text or non-text).
func artifactsHaveAnyParts(artifacts []*a2a.Artifact) bool {
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		if len(artifact.Parts) > 0 {
			return true
		}
	}
	return false
}
