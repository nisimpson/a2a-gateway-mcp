package gateway

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/prop"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nisimpson/a2a-gateway-mcp/directory"
	"github.com/nisimpson/a2a-gateway-mcp/internal/tool"
)

// Feature: discover-agents-default-url, Property 6: Output format equivalence between local and remote paths
// **Validates: Requirements 4.7**

// normalizeOutput sorts agents by name so ordering differences from map iteration
// don't affect structural comparison. The property validates that both paths produce
// identical agent entries (same fields, same values) regardless of iteration order.
func normalizeOutput(output *tool.DiscoverAgentsOutput) *tool.DiscoverAgentsOutput {
	sorted := make([]a2a.AgentCard, len(output.Agents))
	copy(sorted, output.Agents)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})
	return &tool.DiscoverAgentsOutput{
		Agents:    sorted,
		NextToken: output.NextToken,
	}
}

func TestPropertyOutputFormatEquivalenceBetweenLocalAndRemotePaths(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	properties.Property("local and remote paths produce structurally equivalent output", prop.ForAll(
		func(cards []a2a.AgentCard) bool {
			ctx := context.Background()

			// Create a directory and register all cards.
			dir := directory.New()
			for _, c := range cards {
				if err := dir.Register(ctx, c); err != nil {
					return false
				}
			}

			// --- Local path: query via directoryAdapter ---
			localAdapter := &directoryAdapter{dir: dir}
			localTool := &tool.DiscoverAgentsTool{
				HTTPClient: nil, // not needed for local path
				Directory:  localAdapter,
			}

			localInput := &tool.DiscoverAgentsInput{}
			_, localOutput, localErr := localTool.Handle(ctx, &mcp.CallToolRequest{}, localInput)
			if localErr != nil {
				t.Logf("local path error: %v", localErr)
				return false
			}

			// --- Remote path: query via httptest server ---
			ts := httptest.NewServer(dir)
			defer ts.Close()

			remoteTool := &tool.DiscoverAgentsTool{
				HTTPClient:          ts.Client(),
				DefaultDirectoryURL: ts.URL,
			}

			remoteInput := &tool.DiscoverAgentsInput{}
			_, remoteOutput, remoteErr := remoteTool.Handle(ctx, &mcp.CallToolRequest{}, remoteInput)
			if remoteErr != nil {
				t.Logf("remote path error: %v", remoteErr)
				return false
			}

			// --- Compare outputs via JSON normalization (sort by name to handle map iteration order) ---
			localNorm := normalizeOutput(localOutput)
			remoteNorm := normalizeOutput(remoteOutput)

			localJSON, err := json.Marshal(localNorm)
			if err != nil {
				t.Logf("failed to marshal local output: %v", err)
				return false
			}

			remoteJSON, err := json.Marshal(remoteNorm)
			if err != nil {
				t.Logf("failed to marshal remote output: %v", err)
				return false
			}

			if string(localJSON) != string(remoteJSON) {
				t.Logf("output mismatch:\nlocal:  %s\nremote: %s", localJSON, remoteJSON)
				return false
			}

			return true
		},
		genAgentCardSliceForAdapter(),
	))

	properties.TestingRun(t)
}

// Test with filters applied to both paths to confirm equivalence under filtering.
func TestPropertyOutputFormatEquivalenceWithFilter(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	properties.Property("local and remote paths produce equivalent output with filter applied", prop.ForAll(
		func(cards []a2a.AgentCard, filter string) bool {
			ctx := context.Background()

			// Create a directory and register all cards.
			dir := directory.New()
			for _, c := range cards {
				if err := dir.Register(ctx, c); err != nil {
					return false
				}
			}

			// --- Local path: query via directoryAdapter ---
			localAdapter := &directoryAdapter{dir: dir}
			localTool := &tool.DiscoverAgentsTool{
				HTTPClient: nil,
				Directory:  localAdapter,
			}

			localInput := &tool.DiscoverAgentsInput{Filter: filter}
			_, localOutput, localErr := localTool.Handle(ctx, &mcp.CallToolRequest{}, localInput)
			if localErr != nil {
				return false
			}

			// --- Remote path: query via httptest server ---
			ts := httptest.NewServer(dir)
			defer ts.Close()

			remoteTool := &tool.DiscoverAgentsTool{
				HTTPClient:          ts.Client(),
				DefaultDirectoryURL: ts.URL,
			}

			remoteInput := &tool.DiscoverAgentsInput{Filter: filter}
			_, remoteOutput, remoteErr := remoteTool.Handle(ctx, &mcp.CallToolRequest{}, remoteInput)
			if remoteErr != nil {
				return false
			}

			// --- Compare outputs via JSON normalization (sort by name) ---
			localNorm := normalizeOutput(localOutput)
			remoteNorm := normalizeOutput(remoteOutput)

			localJSON, _ := json.Marshal(localNorm)
			remoteJSON, _ := json.Marshal(remoteNorm)

			if string(localJSON) != string(remoteJSON) {
				t.Logf("output mismatch with filter %q:\nlocal:  %s\nremote: %s", filter, localJSON, remoteJSON)
				return false
			}

			return true
		},
		genAgentCardSliceForAdapter(),
		genFilterString(),
	))

	properties.TestingRun(t)
}

// Ensure the test output includes the property feature tag for traceability.
func init() {
	_ = "Feature: discover-agents-default-url, Property 6: Output format equivalence between local and remote paths"
}
