package gateway

import "fmt"

// ResolveResult contains the resolved agent information.
type ResolveResult struct {
	URL     string
	Headers map[string]string
	IsAlias bool   // true if resolved from registry, false if raw URL
	Alias   string // populated when IsAlias is true
}

// ResolveAgent determines whether the identifier is a registered alias or
// a raw URL, and returns the appropriate connection details.
func ResolveAgent(registry *AgentRegistry, identifier string) (*ResolveResult, error) {
	// Step 1: Check if identifier exists as a registered alias.
	if entry := registry.Lookup(identifier); entry != nil {
		return &ResolveResult{
			URL:     entry.URL,
			Headers: entry.Headers,
			IsAlias: true,
			Alias:   identifier,
		}, nil
	}

	// Step 2: Check if identifier is a valid HTTP/HTTPS URL.
	if err := ValidateURL(identifier); err == nil {
		return &ResolveResult{
			URL:     identifier,
			Headers: nil,
			IsAlias: false,
		}, nil
	}

	// Step 3: Neither alias nor valid URL.
	return nil, fmt.Errorf("agent alias not registered and identifier is not a valid URL")
}
