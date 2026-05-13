package gateway

import (
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
			err := ValidateAlias(tt.alias)
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
		{name: "empty string", url: "", wantErr: true},
		{name: "no scheme", url: "example.com", wantErr: true},
		{name: "ftp scheme", url: "ftp://example.com", wantErr: true},
		{name: "ws scheme", url: "ws://example.com", wantErr: true},
		{name: "just path", url: "/api/v1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateURL(tt.url)
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
			err := ValidateMessage(tt.message)
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
			err := ValidateHeaders(tt.headers)
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
			err := ValidateBroadcastAliases(tt.aliases)
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
			err := ValidateAlias(s)
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
			err := ValidateAlias(string(s))
			return err != nil
		},
		gen.IntRange(65, 256),
	))

	properties.TestingRun(t)
}
