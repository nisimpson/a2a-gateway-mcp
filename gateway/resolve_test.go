package gateway

import (
	"fmt"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// Feature: a2a-gateway-mcp, Property 1: Agent identifier resolution consistency
// **Validates: Requirements AGMCP-1.1, AGMCP-1.2, AGMCP-1.3, AGMCP-2.1, AGMCP-2.2, AGMCP-2.3**

func TestPropertyAgentResolutionConsistency(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for valid alias characters
	aliasGen := gen.RegexMatch(`[a-z][a-z0-9\-]{1,10}`)

	// Generator for valid HTTP/HTTPS URLs
	urlGen := gen.OneGenOf(
		gen.RegexMatch(`https://[a-z]{3,10}\\.example\\.com`),
		gen.RegexMatch(`http://[a-z]{3,10}\\.localhost:[0-9]{4}`),
	)

	// Generator for random strings that are neither valid aliases nor valid URLs
	randomStringGen := gen.OneGenOf(
		gen.RegexMatch(`[A-Z][A-Z0-9]{2,10}`),     // uppercase (not a valid alias or URL)
		gen.RegexMatch(`ftp://[a-z]{3,10}\\.com`), // ftp scheme (not http/https)
		gen.RegexMatch(`[a-z]{1,5}_[a-z]{1,5}`),   // underscores (not a valid alias, not a URL)
		gen.Const(""),                             // empty string
		gen.RegexMatch(`[a-z ]{2,8}`),             // spaces (not valid alias or URL)
	)

	// Helper to check if identifier is a valid HTTP/HTTPS URL
	isValidURL := func(id string) bool {
		return ValidateURL(id) == nil
	}

	// Property: For any identifier and registry state, ResolveAgent returns exactly one of three outcomes
	// and the resolution is deterministic.
	properties.Property("ResolveAgent returns exactly one of three outcomes and is deterministic", prop.ForAll(
		func(numAgents int, identifierType int) bool {
			// Build a registry with some agents
			registry := NewAgentRegistry()
			aliases := make([]string, 0, numAgents)
			for i := range numAgents {
				alias := fmt.Sprintf("agent-%d", i)
				url := fmt.Sprintf("https://agent-%d.example.com", i)
				registry.Connect(alias, url, map[string]string{"X-Agent": alias})
				aliases = append(aliases, alias)
			}

			// Choose an identifier based on identifierType
			var identifier string
			switch identifierType % 3 {
			case 0:
				// Use a registered alias (if any exist)
				if len(aliases) > 0 {
					identifier = aliases[identifierType%max(len(aliases), 1)]
				} else {
					identifier = "nonexistent-alias"
				}
			case 1:
				// Use a valid URL not in the registry
				identifier = fmt.Sprintf("https://unregistered-%d.example.com", identifierType)
			case 2:
				// Use an invalid string (neither alias nor URL)
				identifier = fmt.Sprintf("INVALID_%d", identifierType)
			}

			// Call ResolveAgent twice for determinism check
			result1, err1 := ResolveAgent(registry, identifier)
			result2, err2 := ResolveAgent(registry, identifier)

			// Determinism: both calls must produce the same outcome
			if (result1 == nil) != (result2 == nil) {
				return false
			}
			if (err1 == nil) != (err2 == nil) {
				return false
			}
			if result1 != nil && result2 != nil {
				if result1.URL != result2.URL {
					return false
				}
				if result1.IsAlias != result2.IsAlias {
					return false
				}
			}

			// Exactly one of three outcomes must hold
			if result1 != nil && err1 == nil {
				if result1.IsAlias {
					// Outcome (a): identifier is a registered alias
					entry := registry.Lookup(identifier)
					if entry == nil {
						return false // identifier should exist in registry
					}
					if result1.URL != entry.URL {
						return false
					}
				} else {
					// Outcome (b): identifier is a valid HTTP/HTTPS URL not in registry
					if !isValidURL(identifier) {
						return false
					}
					if result1.URL != identifier {
						return false
					}
				}
			} else if err1 != nil && result1 == nil {
				// Outcome (c): neither alias nor valid URL
				entry := registry.Lookup(identifier)
				if entry != nil {
					return false // should NOT be in registry
				}
				if isValidURL(identifier) {
					return false // should NOT be a valid URL
				}
			} else {
				// Invalid state: both result and error, or neither
				return false
			}

			return true
		},
		gen.IntRange(0, 10),  // numAgents
		gen.IntRange(0, 100), // identifierType selector
	))

	// Property with diverse identifier generation
	properties.Property("ResolveAgent with diverse identifiers returns exactly one outcome and is deterministic", prop.ForAll(
		func(aliasEntries []string, identifier string) bool {
			// Build registry from generated aliases
			registry := NewAgentRegistry()
			for _, alias := range aliasEntries {
				if alias != "" {
					url := fmt.Sprintf("https://%s.example.com", alias)
					registry.Connect(alias, url, nil)
				}
			}

			// Call ResolveAgent twice
			result1, err1 := ResolveAgent(registry, identifier)
			result2, err2 := ResolveAgent(registry, identifier)

			// Determinism check
			if (result1 == nil) != (result2 == nil) {
				return false
			}
			if (err1 == nil) != (err2 == nil) {
				return false
			}
			if result1 != nil && result2 != nil {
				if result1.URL != result2.URL || result1.IsAlias != result2.IsAlias {
					return false
				}
			}

			// Exactly one outcome
			if result1 != nil && err1 == nil {
				if result1.IsAlias {
					// Must be in registry
					return registry.Lookup(identifier) != nil
				}
				// Must be a valid URL
				return isValidURL(identifier)
			} else if err1 != nil && result1 == nil {
				// Must NOT be in registry AND must NOT be a valid URL
				return registry.Lookup(identifier) == nil && !isValidURL(identifier)
			}
			// Invalid: both non-nil or both nil
			return false
		},
		gen.SliceOfN(10, aliasGen),
		gen.OneGenOf(
			aliasGen,        // might match a registered alias
			urlGen,          // valid URL
			randomStringGen, // neither alias nor URL
		),
	))

	properties.TestingRun(t)
}
