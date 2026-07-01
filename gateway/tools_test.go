package gateway

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/nisimpson/a2a-gateway-mcp/directory"
)

// --- Generator helpers ---

// genNonEmptyAlpha generates a guaranteed non-empty alpha string.
func genNonEmptyAlpha(params *gopter.GenParameters) string {
	prefix := string(rune('a' + params.NextInt64()%26))
	result, ok := gen.AlphaString()(params).Retrieve()
	if !ok || result == nil {
		return prefix
	}
	return prefix + result.(string)
}

// genAgentCardForAdapter generates a random AgentCard with a name and description.
func genAgentCardForAdapter() gopter.Gen {
	return func(params *gopter.GenParameters) *gopter.GenResult {
		name := genNonEmptyAlpha(params)
		desc := genNonEmptyAlpha(params)

		// Generate 0-2 skills with tags
		raw := params.NextInt64() % 3
		if raw < 0 {
			raw = -raw
		}
		numSkills := int(raw)
		skills := make([]a2a.AgentSkill, numSkills)
		for i := range skills {
			tagCount := int(params.NextInt64()%3 + 1)
			if tagCount < 1 {
				tagCount = 1
			}
			tags := make([]string, tagCount)
			for j := range tags {
				tags[j] = genNonEmptyAlpha(params)
			}
			skills[i] = a2a.AgentSkill{
				ID:          genNonEmptyAlpha(params),
				Name:        genNonEmptyAlpha(params),
				Description: genNonEmptyAlpha(params),
				Tags:        tags,
			}
		}

		card := a2a.AgentCard{
			Name:        name,
			Description: desc,
			Skills:      skills,
		}
		return gopter.NewGenResult(card, gopter.NoShrinker)
	}
}

// genAgentCardSliceForAdapter generates a slice of 1-50 random AgentCards with unique names.
func genAgentCardSliceForAdapter() gopter.Gen {
	return func(params *gopter.GenParameters) *gopter.GenResult {
		raw := params.NextInt64() % 50
		if raw < 0 {
			raw = -raw
		}
		n := int(raw) + 1
		cards := make([]a2a.AgentCard, 0, n)
		seen := make(map[string]bool)
		for i := 0; i < n; i++ {
			result := genAgentCardForAdapter()(params)
			card, _ := result.Retrieve()
			c := card.(a2a.AgentCard)
			if !seen[c.Name] {
				seen[c.Name] = true
				cards = append(cards, c)
			}
		}
		if len(cards) == 0 {
			cards = append(cards, a2a.AgentCard{
				Name:        fmt.Sprintf("fallback-%d", params.NextInt64()),
				Description: "fallback description",
			})
		}
		return gopter.NewGenResult(cards, gopter.NoShrinker)
	}
}

// genFilterString generates a random filter string (short alphanumeric).
func genFilterString() gopter.Gen {
	return func(params *gopter.GenParameters) *gopter.GenResult {
		// Generate a short filter string (1-5 chars) from alpha chars
		length := int(params.NextInt64()%5) + 1
		if length < 1 {
			length = 1
		}
		var sb strings.Builder
		for i := 0; i < length; i++ {
			ch := rune('a' + params.NextInt64()%26)
			sb.WriteRune(ch)
		}
		return gopter.NewGenResult(sb.String(), gopter.NoShrinker)
	}
}

// matchesCardForFilter checks if a card matches the filter using the same logic as DefaultResolver:
// case-insensitive substring match on name, description, or skill tags.
func matchesCardForFilter(filter string, card a2a.AgentCard) bool {
	q := strings.ToLower(filter)
	if strings.Contains(strings.ToLower(card.Name), q) {
		return true
	}
	if strings.Contains(strings.ToLower(card.Description), q) {
		return true
	}
	for _, skill := range card.Skills {
		for _, tag := range skill.Tags {
			if strings.Contains(strings.ToLower(tag), q) {
				return true
			}
		}
	}
	return false
}

// --- Property-Based Tests ---

// Feature: discover-agents-default-url, Property 4: Self-hosted directory filtering preserves invariants
// **Validates: Requirements 4.1, 4.2**

func TestPropertySelfHostedDirectoryFilteringPreservesInvariants(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	properties.Property("result is a subset of input and every result card matches the filter", prop.ForAll(
		func(cards []a2a.AgentCard, filter string) bool {
			// Create a directory and register all cards.
			dir := directory.New()
			ctx := context.Background()

			for _, c := range cards {
				if err := dir.Register(ctx, c); err != nil {
					return false
				}
			}

			// Create the directoryAdapter wrapping the directory.
			adapter := &directoryAdapter{dir: dir}

			// Query via the adapter with the random filter.
			result, err := adapter.Query(ctx, filter, 0, "", false)
			if err != nil {
				return false
			}

			// Build a set of original card names for subset check.
			originalNames := make(map[string]bool)
			for _, c := range cards {
				originalNames[c.Name] = true
			}

			// Verify invariant 1: every card in result exists in the original set (subset check).
			for _, rc := range result.Cards {
				if !originalNames[rc.Name] {
					return false
				}
			}

			// Verify invariant 2: every card in result matches the filter.
			for _, rc := range result.Cards {
				if !matchesCardForFilter(filter, rc) {
					return false
				}
			}

			return true
		},
		genAgentCardSliceForAdapter(),
		genFilterString(),
	))

	properties.TestingRun(t)
}
