package gateway

import (
	"context"
	"fmt"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/nisimpson/a2a-gateway-mcp/directory"
)

// genAgentCardForGateway generates a random AgentCard with a unique name.
func genAgentCardForGateway(idx int, params *gopter.GenParameters) a2a.AgentCard {
	// Use index to guarantee unique names across a card set.
	suffix := params.NextInt64()
	if suffix < 0 {
		suffix = -suffix
	}
	return a2a.AgentCard{
		Name:        fmt.Sprintf("agent-%d-%d", idx, suffix),
		Description: fmt.Sprintf("Description for agent %d", idx),
	}
}

// Feature: discover-agents-default-url, Property 5: Limit bounds the result size
// **Validates: Requirements 4.3**

func TestPropertyLimitBoundsResultSize(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	properties.Property("len(result.Cards) <= limit for any positive limit and card set", prop.ForAll(
		func(numCards int, limit int) bool {
			// Create a directory with numCards registered cards.
			dir := directory.New()
			ctx := context.Background()

			for i := 0; i < numCards; i++ {
				card := a2a.AgentCard{
					Name:        fmt.Sprintf("agent-%d", i),
					Description: fmt.Sprintf("Agent number %d", i),
				}
				if err := dir.Register(ctx, card); err != nil {
					return false
				}
			}

			// Create the directoryAdapter wrapping the directory.
			adapter := &directoryAdapter{dir: dir}

			// Query via the adapter with the generated limit.
			result, err := adapter.Query(ctx, "", limit, "", false)
			if err != nil {
				return false
			}

			// Property: len(result.Cards) <= limit
			return len(result.Cards) <= limit
		},
		gen.IntRange(0, 200), // numCards: 0-200
		gen.IntRange(1, 100), // limit: 1-100
	))

	properties.TestingRun(t)
}
