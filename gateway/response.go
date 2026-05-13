package gateway

import (
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// FormatTaskResponse extracts text content from an A2A Task and formats it
// as MCP CallToolResult content items.
// Requirement: AGMCP-7.1, AGMCP-7.2, AGMCP-7.3, AGMCP-7.4, AGMCP-7.5 — response formatting
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
