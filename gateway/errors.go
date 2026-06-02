package gateway

import (
	"errors"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// handleA2AError classifies an error returned by the a2aclient SDK and
// returns an appropriate MCP error result. It uses errors.Is to match
// sentinel errors from the SDK.
func handleA2AError(err error) *mcp.CallToolResult {
	switch {
	case errors.Is(err, a2a.ErrTaskNotFound):
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "task not found"}}}
	case errors.Is(err, a2a.ErrTaskNotCancelable):
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "task is not cancelable"}}}
	default:
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}
	}
}
