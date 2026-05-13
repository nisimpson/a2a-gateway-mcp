package gateway

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// capturingTransport is a mock RoundTripper that captures the request headers for verification.
type capturingTransport struct {
	captured http.Header
}

func (c *capturingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.captured = req.Header.Clone()
	return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
}

// Feature: a2a-gateway-mcp, Property 10: Header injection composition order
// **Validates: Requirements AGMCP-14.2, AGMCP-14.4, AGMCP-14.6, AGMCP-14.8, AGMCP-15.4, AGMCP-15.6**

func TestPropertyHeaderInjectionComposition(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for a header map with 1-20 entries including possible protocol headers
	headerMapGen := gopter.CombineGens(
		gen.IntRange(1, 20),
		gen.Bool(), // include Content-Type?
		gen.Bool(), // include Accept?
	).FlatMap(func(v interface{}) gopter.Gen {
		values := v.([]interface{})
		numHeaders := values[0].(int)
		includeContentType := values[1].(bool)
		includeAccept := values[2].(bool)

		return gen.SliceOfN(numHeaders, gen.RegexMatch(`[a-z0-9]{1,10}`)).Map(func(keys []string) map[string]string {
			headers := make(map[string]string)
			for i, k := range keys {
				headerKey := fmt.Sprintf("X-Custom-%s-%d", k, i)
				headers[headerKey] = fmt.Sprintf("value-%d", i)
			}
			if includeContentType {
				headers["Content-Type"] = "text/plain"
			}
			if includeAccept {
				headers["Accept"] = "text/html"
			}
			return headers
		})
	}, reflect.TypeOf(map[string]string{}))

	properties.Property("headerRoundTripper injects non-protocol headers and skips protocol-reserved ones", prop.ForAll(
		func(headers map[string]string) bool {
			// Create a capturing transport as the base
			capture := &capturingTransport{}

			// Create the headerRoundTripper wrapping the capturing transport
			hrt := &headerRoundTripper{
				base:    capture,
				headers: headers,
			}

			// Create a request with original protocol headers
			req, err := http.NewRequest("POST", "https://agent.example.com/a2a", nil)
			if err != nil {
				return false
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json")

			// Execute the RoundTrip
			_, err = hrt.RoundTrip(req)
			if err != nil {
				return false
			}

			// Verify: all non-protocol headers from the map are present on the captured request
			for k, v := range headers {
				if isProtocolHeader(k) {
					continue
				}
				capturedVal := capture.captured.Get(k)
				if capturedVal != v {
					return false
				}
			}

			// Verify: protocol headers retain their original values (not overridden)
			if capture.captured.Get("Content-Type") != "application/json" {
				return false
			}
			if capture.captured.Get("Accept") != "application/json" {
				return false
			}

			// Verify: the original request is not mutated (clone behavior)
			if req.Header.Get("Content-Type") != "application/json" {
				return false
			}
			if req.Header.Get("Accept") != "application/json" {
				return false
			}
			// Original request should NOT have the injected custom headers
			for k := range headers {
				if isProtocolHeader(k) {
					continue
				}
				if req.Header.Get(k) != "" {
					return false
				}
			}

			return true
		},
		headerMapGen,
	))

	properties.TestingRun(t)
}


// Feature: a2a-gateway-mcp, Property 11: URL-based requests bypass header injection
// **Validates: Requirements AGMCP-14.7, AGMCP-13.8**

func TestPropertyURLBypassesHeaderInjection(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for valid alias characters: lowercase letters, digits, hyphens
	aliasGen := gen.RegexMatch(`[a-z][a-z0-9\-]{1,10}`)

	// Generator for header key-value pairs (non-protocol headers)
	headerKeyGen := gen.RegexMatch(`X-[A-Za-z]{1,10}`)
	headerValGen := gen.RegexMatch(`[a-zA-Z0-9]{1,20}`)

	properties.Property("URL-based resolution returns no headers even when matching URL exists in registry", prop.ForAll(
		func(alias string, urlPath string, headerKey string, headerVal string) bool {
			if alias == "" || urlPath == "" || headerKey == "" || headerVal == "" {
				return true // skip degenerate cases
			}

			// Build a valid URL from the generated path component
			agentURL := fmt.Sprintf("https://%s.example.com/%s", alias, urlPath)

			// Create a registry and register the agent with headers
			registry := NewAgentRegistry()
			headers := map[string]string{headerKey: headerVal}
			registry.Connect(alias, agentURL, headers)

			// Resolve using the raw URL (not the alias)
			result, err := ResolveAgent(registry, agentURL)
			if err != nil {
				// The URL should be valid and resolvable
				return false
			}

			// Verify: IsAlias must be false since we used the URL directly
			if result.IsAlias {
				return false
			}

			// Verify: Headers must be nil for URL-based resolution
			if result.Headers != nil {
				return false
			}

			// Verify: httpClientForAgent with an entry that has empty headers
			// returns the base client (no header injection)
			baseClient := &http.Client{}
			emptyEntry := &AgentEntry{
				Alias:   alias,
				URL:     agentURL,
				Headers: nil,
			}
			clientForEmpty := httpClientForAgent(baseClient, emptyEntry)
			if clientForEmpty != baseClient {
				return false
			}

			// Also verify with explicitly empty map
			emptyMapEntry := &AgentEntry{
				Alias:   alias,
				URL:     agentURL,
				Headers: map[string]string{},
			}
			clientForEmptyMap := httpClientForAgent(baseClient, emptyMapEntry)
			return clientForEmptyMap == baseClient
		},
		aliasGen,
		gen.RegexMatch(`[a-z]{1,10}`), // urlPath
		headerKeyGen,
		headerValGen,
	))

	properties.Property("URL-based resolution never inherits registry headers regardless of URL match", prop.ForAll(
		func(aliases []string, headerCount int) bool {
			// Deduplicate aliases
			seen := make(map[string]bool)
			unique := make([]string, 0, len(aliases))
			for _, a := range aliases {
				if !seen[a] && a != "" {
					seen[a] = true
					unique = append(unique, a)
				}
			}

			if len(unique) == 0 {
				return true
			}

			registry := NewAgentRegistry()

			// Register all agents with headers
			for i, alias := range unique {
				url := fmt.Sprintf("https://%s.example.com/agent/%d", alias, i)
				headers := make(map[string]string)
				for j := 0; j < headerCount; j++ {
					headers[fmt.Sprintf("X-Header-%d", j)] = fmt.Sprintf("value-%d", j)
				}
				registry.Connect(alias, url, headers)
			}

			// For each registered agent, resolve using the raw URL
			for i, alias := range unique {
				url := fmt.Sprintf("https://%s.example.com/agent/%d", alias, i)

				result, err := ResolveAgent(registry, url)
				if err != nil {
					return false
				}

				// Must NOT be resolved as alias (even though URL matches a registered agent)
				if result.IsAlias {
					return false
				}

				// Must have nil headers
				if result.Headers != nil {
					return false
				}

				// httpClientForAgent with nil headers returns base client
				baseClient := &http.Client{}
				entry := &AgentEntry{
					Alias:   alias,
					URL:     url,
					Headers: result.Headers, // nil from URL-based resolution
				}
				client := httpClientForAgent(baseClient, entry)
				if client != baseClient {
					return false
				}
			}

			return true
		},
		gen.SliceOfN(10, aliasGen).SuchThat(func(v interface{}) bool {
			s := v.([]string)
			return len(s) >= 1
		}),
		gen.IntRange(1, 5), // headerCount
	))

	properties.TestingRun(t)
}
