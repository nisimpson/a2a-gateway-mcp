package gateway

import (
	"fmt"
	"net/url"
)

const (
	maxAliasLength      = 64
	maxMessageLength    = 32768
	maxHeaderEntries    = 20
	maxBroadcastAliases = 20
	minBroadcastAliases = 1
)

// ValidateAlias checks that the alias contains only lowercase letters, digits, and hyphens,
// and is between 1 and 64 characters.
func ValidateAlias(alias string) error {
	if alias == "" {
		return fmt.Errorf("alias is required and cannot be empty")
	}
	if len(alias) > maxAliasLength {
		return fmt.Errorf("alias must be at most %d characters, got %d", maxAliasLength, len(alias))
	}
	for i, ch := range alias {
		if !isValidAliasChar(ch) {
			return fmt.Errorf("alias contains invalid character %q at position %d: only lowercase letters, digits, and hyphens are allowed", ch, i)
		}
	}
	return nil
}

// isValidAliasChar returns true if the rune is a lowercase letter, digit, or hyphen.
func isValidAliasChar(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-'
}

// ValidateURL checks that the URL has an http or https scheme.
// Returns an error if the URL is empty or has an invalid scheme.
func ValidateURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("URL is required and cannot be empty")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("URL is malformed: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("URL must have http or https scheme, got %q", parsed.Scheme)
	}
	return nil
}

// ValidateMessage checks that the message is non-empty and at most 32768 characters.
// Returns an error if invalid.
func ValidateMessage(message string) error {
	if message == "" {
		return fmt.Errorf("message is required and cannot be empty")
	}
	if len(message) > maxMessageLength {
		return fmt.Errorf("message must be at most %d characters, got %d", maxMessageLength, len(message))
	}
	return nil
}

// ValidateHeaders checks that the headers map has at most 20 entries.
// Returns an error if there are too many headers.
func ValidateHeaders(headers map[string]string) error {
	if len(headers) > maxHeaderEntries {
		return fmt.Errorf("headers must have at most %d entries, got %d", maxHeaderEntries, len(headers))
	}
	return nil
}

// ValidateBroadcastAliases checks that the aliases slice has between 1 and 20 items.
// Returns an error if empty or exceeds the maximum.
func ValidateBroadcastAliases(aliases []string) error {
	if len(aliases) < minBroadcastAliases {
		return fmt.Errorf("at least %d target alias is required for broadcast", minBroadcastAliases)
	}
	if len(aliases) > maxBroadcastAliases {
		return fmt.Errorf("broadcast targets must be at most %d aliases, got %d", maxBroadcastAliases, len(aliases))
	}
	return nil
}
