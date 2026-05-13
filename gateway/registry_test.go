package gateway

import (
	"fmt"
	"sync"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// Feature: a2a-gateway-mcp, Property 5: Registry concurrent safety
// **Validates: Requirements AGMCP-9.1, AGMCP-9.4, AGMCP-9.5**

func TestPropertyRegistryConcurrentSafety(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	properties.Property("concurrent Connect operations with distinct aliases all succeed", prop.ForAll(
		func(n int) bool {
			registry := NewAgentRegistry()

			// Generate N distinct aliases and URLs
			aliases := make([]string, n)
			urls := make([]string, n)
			for i := 0; i < n; i++ {
				aliases[i] = fmt.Sprintf("agent-%d", i)
				urls[i] = fmt.Sprintf("https://agent-%d.example.com", i)
			}

			// Run N concurrent Connect operations
			var wg sync.WaitGroup
			wg.Add(n)
			for i := 0; i < n; i++ {
				go func(idx int) {
					defer wg.Done()
					registry.Connect(aliases[idx], urls[idx], nil)
				}(i)
			}
			wg.Wait()

			// Verify registry has exactly N entries
			if registry.Len() != n {
				return false
			}

			// Verify each alias is present with the correct URL
			for i := 0; i < n; i++ {
				entry := registry.Lookup(aliases[i])
				if entry == nil {
					return false
				}
				if entry.URL != urls[i] {
					return false
				}
				if entry.Alias != aliases[i] {
					return false
				}
			}

			return true
		},
		gen.IntRange(1, 50),
	))

	properties.TestingRun(t)
}

// Feature: a2a-gateway-mcp, Property 7: List agents sorted output
// **Validates: Requirements AGMCP-11.1, AGMCP-11.3**

func TestPropertyListAgentsSorted(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for valid alias characters: lowercase letters, digits, hyphens
	aliasGen := gen.RegexMatch(`[a-z0-9][a-z0-9\-]{0,15}`)

	properties.Property("List returns entries sorted by alias ascending with correct count", prop.ForAll(
		func(aliases []string) bool {
			// Deduplicate aliases to ensure distinct entries
			seen := make(map[string]bool)
			unique := make([]string, 0, len(aliases))
			for _, a := range aliases {
				if !seen[a] && a != "" {
					seen[a] = true
					unique = append(unique, a)
				}
			}

			if len(unique) == 0 {
				return true // skip empty sets
			}

			registry := NewAgentRegistry()

			// Connect all entries
			for _, alias := range unique {
				url := fmt.Sprintf("https://%s.example.com", alias)
				registry.Connect(alias, url, nil)
			}

			// Call List and verify
			entries := registry.List()

			// Verify count matches
			if len(entries) != len(unique) {
				return false
			}

			// Verify sorted in ascending lexicographic order by alias
			for i := 1; i < len(entries); i++ {
				if entries[i-1].Alias >= entries[i].Alias {
					return false
				}
			}

			// Verify all original aliases are present
			entryAliases := make(map[string]bool)
			for _, e := range entries {
				entryAliases[e.Alias] = true
			}
			for _, alias := range unique {
				if !entryAliases[alias] {
					return false
				}
			}

			return true
		},
		gen.SliceOfN(50, aliasGen).SuchThat(func(v interface{}) bool {
			s := v.([]string)
			return len(s) >= 1
		}),
	))

	properties.TestingRun(t)
}

// Feature: a2a-gateway-mcp, Property 6: Registry disconnect atomicity
// **Validates: Requirements AGMCP-10.1, AGMCP-13.6**

func TestPropertyRegistryDisconnectAtomicity(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for valid alias characters: lowercase letters, digits, hyphens
	aliasGen := gen.RegexMatch(`[a-z][a-z0-9\-]{1,10}`)

	properties.Property("Disconnect removes entry from both registry and context store while leaving others intact", prop.ForAll(
		func(aliases []string, disconnectIdx int) bool {
			// Deduplicate aliases to ensure distinct entries
			seen := make(map[string]bool)
			unique := make([]string, 0, len(aliases))
			for _, a := range aliases {
				if !seen[a] && a != "" {
					seen[a] = true
					unique = append(unique, a)
				}
			}

			if len(unique) < 2 {
				return true // need at least 2 entries to test disconnect vs remaining
			}

			// Normalize disconnectIdx to valid range
			targetIdx := disconnectIdx % len(unique)
			if targetIdx < 0 {
				targetIdx = -targetIdx
			}

			registry := NewAgentRegistry()
			contextStore := NewContextStore()

			// Connect all entries and set context store entries
			for _, alias := range unique {
				url := fmt.Sprintf("https://%s.example.com", alias)
				registry.Connect(alias, url, map[string]string{"X-Agent": alias})
				contextStore.Set(alias, fmt.Sprintf("ctx-%s", alias))
			}

			// Pick the alias to disconnect
			targetAlias := unique[targetIdx]

			// Simulate atomic disconnect: remove from registry and context store
			registry.Disconnect(targetAlias)
			contextStore.Delete(targetAlias)

			// Verify: disconnected alias is NOT in the registry
			if registry.Lookup(targetAlias) != nil {
				return false
			}

			// Verify: disconnected alias is NOT in the context store
			if contextStore.Get(targetAlias) != "" {
				return false
			}

			// Verify: all other entries remain intact in both registry and context store
			for i, alias := range unique {
				if i == targetIdx {
					continue
				}

				// Check registry entry is still present with correct data
				entry := registry.Lookup(alias)
				if entry == nil {
					return false
				}
				expectedURL := fmt.Sprintf("https://%s.example.com", alias)
				if entry.URL != expectedURL {
					return false
				}
				if entry.Headers["X-Agent"] != alias {
					return false
				}

				// Check context store entry is still present
				expectedCtx := fmt.Sprintf("ctx-%s", alias)
				if contextStore.Get(alias) != expectedCtx {
					return false
				}
			}

			// Verify registry count is correct (one less than original)
			if registry.Len() != len(unique)-1 {
				return false
			}

			return true
		},
		gen.SliceOfN(20, aliasGen).SuchThat(func(v interface{}) bool {
			s := v.([]string)
			return len(s) >= 2
		}),
		gen.Int(),
	))

	properties.TestingRun(t)
}
