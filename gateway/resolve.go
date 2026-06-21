package gateway

// ResolveResult contains the resolved agent information.
type ResolveResult struct {
	URL     string
	Headers map[string]string
	IsAlias bool   // true if resolved from registry, false if raw URL
	Alias   string // populated when IsAlias is true
}
