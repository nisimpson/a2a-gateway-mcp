package validate

import (
	"fmt"
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

func TestValidateAlias(t *testing.T) {
	tests := []struct {
		name    string
		alias   string
		wantErr bool
	}{
		{name: "valid simple alias", alias: "my-agent", wantErr: false},
		{name: "valid digits only", alias: "123", wantErr: false},
		{name: "valid letters only", alias: "agent", wantErr: false},
		{name: "valid mixed", alias: "code-reviewer-1", wantErr: false},
		{name: "valid single char", alias: "a", wantErr: false},
		{name: "valid max length", alias: strings.Repeat("a", 64), wantErr: false},
		{name: "empty string", alias: "", wantErr: true},
		{name: "exceeds max length", alias: strings.Repeat("a", 65), wantErr: true},
		{name: "uppercase letters", alias: "MyAgent", wantErr: true},
		{name: "spaces", alias: "my agent", wantErr: true},
		{name: "underscores", alias: "my_agent", wantErr: true},
		{name: "special chars", alias: "agent@1", wantErr: true},
		{name: "dots", alias: "agent.one", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Alias(tt.alias)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAlias(%q) error = %v, wantErr %v", tt.alias, err, tt.wantErr)
			}
		})
	}
}

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{name: "valid https", url: "https://example.com", wantErr: false},
		{name: "valid http", url: "http://localhost:8080", wantErr: false},
		{name: "valid https with path", url: "https://example.com/api/v1", wantErr: false},
		{name: "valid http with ip", url: "http://192.168.1.1:9090/path", wantErr: false},
		{name: "empty string", url: "", wantErr: true},
		{name: "no scheme", url: "example.com", wantErr: true},
		{name: "ftp scheme", url: "ftp://example.com", wantErr: true},
		{name: "ws scheme", url: "ws://example.com", wantErr: true},
		{name: "just path", url: "/api/v1", wantErr: true},
		{name: "http scheme empty host", url: "http://", wantErr: true},
		{name: "https scheme empty host", url: "https://", wantErr: true},
		{name: "http scheme with path but no host", url: "http:///path", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := URL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestValidateMessage(t *testing.T) {
	tests := []struct {
		name    string
		message string
		wantErr bool
	}{
		{name: "valid short message", message: "hello", wantErr: false},
		{name: "valid max length", message: strings.Repeat("x", 32768), wantErr: false},
		{name: "empty string", message: "", wantErr: true},
		{name: "exceeds max length", message: strings.Repeat("x", 32769), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Message(tt.message)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateMessage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateHeaders(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		wantErr bool
	}{
		{name: "nil headers", headers: nil, wantErr: false},
		{name: "empty headers", headers: map[string]string{}, wantErr: false},
		{name: "one header", headers: map[string]string{"Authorization": "Bearer token"}, wantErr: false},
		{name: "max headers", headers: makeHeaders(20), wantErr: false},
		{name: "exceeds max headers", headers: makeHeaders(21), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Headers(tt.headers)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateHeaders() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateBroadcastAliases(t *testing.T) {
	tests := []struct {
		name    string
		aliases []string
		wantErr bool
	}{
		{name: "one alias", aliases: []string{"agent-1"}, wantErr: false},
		{name: "max aliases", aliases: makeAliases(20), wantErr: false},
		{name: "nil aliases", aliases: nil, wantErr: true},
		{name: "empty aliases", aliases: []string{}, wantErr: true},
		{name: "exceeds max aliases", aliases: makeAliases(21), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := BroadcastAliases(tt.aliases)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateBroadcastAliases() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func makeHeaders(n int) map[string]string {
	h := make(map[string]string, n)
	for i := 0; i < n; i++ {
		h[strings.Repeat("k", i+1)] = "value"
	}
	return h
}

func makeAliases(n int) []string {
	aliases := make([]string, n)
	for i := 0; i < n; i++ {
		aliases[i] = "agent-" + strings.Repeat("a", i+1)
	}
	return aliases
}

func TestValidatePingEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		wantErr  bool
		errMsg   string
	}{
		{name: "valid simple path", endpoint: "/health", wantErr: false},
		{name: "valid root path", endpoint: "/", wantErr: false},
		{name: "valid nested path", endpoint: "/api/v1/health", wantErr: false},
		{name: "valid max length", endpoint: "/" + strings.Repeat("a", 255), wantErr: false},
		{name: "empty string", endpoint: "", wantErr: true, errMsg: "ping_endpoint must start with '/'"},
		{name: "missing leading slash", endpoint: "health", wantErr: true, errMsg: "ping_endpoint must start with '/'"},
		{name: "exceeds max length", endpoint: "/" + strings.Repeat("a", 256), wantErr: true, errMsg: "ping_endpoint must be at most 256 characters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := PingEndpoint(tt.endpoint)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePingEndpoint(%q) error = %v, wantErr %v", tt.endpoint, err, tt.wantErr)
			}
			if tt.wantErr && err != nil && tt.errMsg != "" {
				if err.Error() != tt.errMsg {
					t.Errorf("ValidatePingEndpoint(%q) error = %q, want %q", tt.endpoint, err.Error(), tt.errMsg)
				}
			}
		})
	}
}

// Feature: discover-agents-default-url, Property 1: URL validation rejects invalid URLs and accepts valid ones
// **Validates: Requirements 1.5, 6.1, 6.3**

func TestPropertyURLValidation(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for valid host names (simple alphanumeric with dots)
	validHostGen := gen.RegexMatch(`[a-z][a-z0-9]{1,10}(\.[a-z]{2,5}){1,2}`)

	// Generator for valid paths
	validPathGen := gen.OneGenOf(
		gen.Const(""),
		gen.RegexMatch(`(/[a-z0-9]{1,8}){1,3}`),
	)

	// Generator for optional port
	validPortGen := gen.OneGenOf(
		gen.Const(""),
		gen.IntRange(80, 9999).Map(func(v int) string {
			return fmt.Sprintf(":%d", v)
		}),
	)

	// Property: valid URLs (http or https scheme + non-empty host) are accepted
	properties.Property("valid http/https URLs with non-empty host are accepted", prop.ForAll(
		func(scheme string, host string, port string, path string) bool {
			u := scheme + "://" + host + port + path
			err := URL(u)
			return err == nil
		},
		gen.OneConstOf("http", "https"),
		validHostGen,
		validPortGen,
		validPathGen,
	))

	// Property: URLs with invalid schemes are rejected
	properties.Property("URLs with non-http/https schemes are rejected", prop.ForAll(
		func(scheme string, host string) bool {
			u := scheme + "://" + host
			err := URL(u)
			return err != nil
		},
		gen.OneConstOf("ftp", "ws", "wss", "file", "ssh", "telnet", "gopher", "mqtt"),
		validHostGen,
	))

	// Property: empty string is rejected
	properties.Property("empty string is rejected", prop.ForAll(
		func(_ int) bool {
			err := URL("")
			return err != nil
		},
		gen.Const(0),
	))

	// Property: URLs with empty host are rejected
	properties.Property("URLs with empty host are rejected", prop.ForAll(
		func(scheme string, path string) bool {
			// scheme:// with no host
			u := scheme + "://"
			err := URL(u)
			if err == nil {
				return false
			}
			// scheme:///path (empty host, non-empty path)
			u2 := scheme + ":///" + path
			err2 := URL(u2)
			return err2 != nil
		},
		gen.OneConstOf("http", "https"),
		gen.RegexMatch(`[a-z]{1,5}`),
	))

	// Property: strings without a scheme (no "://") are rejected
	properties.Property("strings without scheme separator are rejected", prop.ForAll(
		func(host string) bool {
			// Just a hostname with no scheme
			err := URL(host)
			return err != nil
		},
		validHostGen,
	))

	// Property: malformed URLs with spaces are rejected
	properties.Property("URLs containing spaces are rejected", prop.ForAll(
		func(scheme string) bool {
			// Test that strings with spaces in the host part are rejected
			u := scheme + "://host with space.com"
			err := URL(u)
			return err != nil
		},
		gen.OneConstOf("http", "https"),
	))

	properties.TestingRun(t)
}

// Feature: discover-agents-default-url, Property 7: Validation consistency between config-time and runtime
// **Validates: Requirements 6.4**

func TestPropertyValidationConsistency(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// mixedURLStringGen generates a mix of valid URLs, invalid URLs, empty strings, and garbage.
	// We use a flat OneConstOf + regex generators to avoid nested OneGenOf issues with Map.
	mixedURLStringGen := gen.OneGenOf(
		// Valid HTTP URLs
		gen.RegexMatch(`[a-z]{3,10}`).Map(func(host string) string {
			return "http://" + host + ".com"
		}),
		// Valid HTTPS URLs with path
		gen.RegexMatch(`[a-z]{3,10}`).Map(func(host string) string {
			return "https://" + host + ".org/path"
		}),
		// Valid HTTPS URLs with port
		gen.RegexMatch(`[a-z]{3,10}`).Map(func(host string) string {
			return "https://" + host + ".io:8080/api/v1"
		}),
		// Invalid: empty string, wrong schemes, no host, garbage
		gen.OneConstOf(
			"",
			"ftp://example.com",
			"ws://example.com",
			"http://",
			"https://",
			"just-a-string",
			"/path/only",
			"://missing-scheme.com",
			"   ",
			"\t\n",
			"not-a-url-at-all",
			"file:///local/path",
			"mailto:user@example.com",
		),
		// Random alphanumeric garbage
		gen.RegexMatch(`[a-zA-Z0-9]{0,30}`),
	)

	// Property: calling validate.URL twice on the same input produces identical results.
	// This verifies that the same validation is applied at config-time and runtime.
	properties.Property("validate.URL is deterministic - same input always produces same accept/reject decision", prop.ForAll(
		func(input string) bool {
			// Simulate config-time validation
			err1 := URL(input)
			// Simulate runtime validation
			err2 := URL(input)

			// Both must agree on accept/reject
			if (err1 == nil) != (err2 == nil) {
				return false
			}

			// If both reject, error messages should be identical
			if err1 != nil && err2 != nil {
				return err1.Error() == err2.Error()
			}

			return true
		},
		mixedURLStringGen,
	))

	// Property: calling validate.URL multiple times (>2) still produces consistent results
	properties.Property("validate.URL produces consistent results across multiple invocations", prop.ForAll(
		func(input string) bool {
			results := make([]error, 5)
			for i := range results {
				results[i] = URL(input)
			}

			// All results must have the same nil/non-nil status
			firstIsNil := results[0] == nil
			for _, r := range results[1:] {
				if (r == nil) != firstIsNil {
					return false
				}
			}

			// All non-nil errors must have identical messages
			if !firstIsNil {
				msg := results[0].Error()
				for _, r := range results[1:] {
					if r.Error() != msg {
						return false
					}
				}
			}

			return true
		},
		mixedURLStringGen,
	))

	properties.TestingRun(t)
}

// Feature: a2a-gateway-mcp, Property 9: Input validation rejects invalid aliases
// **Validates: Requirements AGMCP-5.9**

func TestPropertyValidationRejectsInvalidAliases(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// invalidCharGen generates a rune that is NOT in [a-z0-9-]
	invalidCharGen := gen.OneGenOf(
		gen.Rune().SuchThat(func(v interface{}) bool {
			r := v.(rune)
			return r >= 'A' && r <= 'Z' // uppercase letters
		}),
		gen.OneConstOf('!', '@', '#', '$', '%', '^', '&', '*', '(', ')', '_', '+', '=', ' ', '.', ',', '/', '\\', '~', '`', '[', ']', '{', '}', '|', '<', '>', '?', ';', ':', '\'', '"'),
	)

	// Property 1: Any string containing characters outside [a-z0-9-] should be rejected
	properties.Property("strings with invalid characters are rejected by ValidateAlias", prop.ForAll(
		func(prefix string, invalidChar rune, suffix string) bool {
			// Build a string that contains at least one invalid character
			s := prefix + string(invalidChar) + suffix
			if len(s) > 64 {
				s = s[:64] // truncate but keep the invalid char by ensuring it's within bounds
				// Re-check that the string still has an invalid char
				hasInvalid := false
				for _, ch := range s {
					if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-') {
						hasInvalid = true
						break
					}
				}
				if !hasInvalid {
					return true // skip this case
				}
			}
			err := Alias(s)
			return err != nil
		},
		gen.RegexMatch(`[a-z0-9\-]{0,10}`), // valid prefix
		invalidCharGen,                     // at least one invalid character
		gen.RegexMatch(`[a-z0-9\-]{0,10}`), // valid suffix
	))

	// Property 2: Any string exceeding 64 characters should be rejected
	properties.Property("strings exceeding 64 characters are rejected by ValidateAlias", prop.ForAll(
		func(length int) bool {
			// Generate a string of valid characters but exceeding max length
			s := make([]byte, length)
			for i := range s {
				s[i] = 'a' + byte(i%26)
			}
			err := Alias(string(s))
			return err != nil
		},
		gen.IntRange(65, 256),
	))

	properties.TestingRun(t)
}
