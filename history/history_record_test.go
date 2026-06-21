package history

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// Feature: task-history, Property 2: Truncation bounds text length
// **Validates: Requirements 1.3, 6.3**

func TestPropertyTruncationBoundsTextLength(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for random strings (0-5000 runes).
	// Uses a mix of ASCII (via AlphaString) and multi-byte unicode (CJK chars)
	// to exercise rune-aware truncation logic.
	stringGen := gen.IntRange(0, 5000).Map(func(length int) string {
		// Build a string with a known rune count mixing ASCII and multi-byte chars
		var sb strings.Builder
		for i := range length {
			if i%3 == 0 {
				// Multi-byte CJK character (3 bytes in UTF-8, 1 rune)
				sb.WriteRune(rune(0x4E00 + (i % 1000)))
			} else {
				// ASCII character (1 byte, 1 rune)
				sb.WriteRune(rune('a' + (i % 26)))
			}
		}
		return sb.String()
	})

	// Generator for max lengths (1-2000)
	maxLenGen := gen.IntRange(1, 2000)

	// Sub-property 1: If len([]rune(s)) <= M, result equals original string unchanged
	properties.Property("If rune length of input <= maxLen, truncateText returns the original string unchanged", prop.ForAll(
		func(s string, maxLen int) bool {
			runes := []rune(s)

			// Only test when input fits within maxLen
			if len(runes) > maxLen {
				return true // skip this case; tested by other properties
			}

			result := truncateText(s, maxLen)
			if result != s {
				t.Logf("Expected original string unchanged, got %q (input len %d runes, maxLen %d)",
					result, len(runes), maxLen)
				return false
			}
			return true
		},
		stringGen,
		maxLenGen,
	))

	// Sub-property 2: If len([]rune(s)) > M, result has rune length exactly M+1 (M runes + "…")
	properties.Property("If rune length of input > maxLen, result has rune length exactly maxLen+1", prop.ForAll(
		func(s string, maxLen int) bool {
			runes := []rune(s)

			// Only test when input exceeds maxLen
			if len(runes) <= maxLen {
				return true // skip this case; tested by other property
			}

			result := truncateText(s, maxLen)
			resultRunes := []rune(result)

			// Result rune length should be exactly M + 1 (M runes + 1 ellipsis rune)
			expectedLen := maxLen + 1
			if len(resultRunes) != expectedLen {
				t.Logf("Expected rune length %d, got %d (input rune len %d, maxLen %d)",
					expectedLen, len(resultRunes), len(runes), maxLen)
				return false
			}
			return true
		},
		stringGen,
		maxLenGen,
	))

	// Sub-property 3: If len([]rune(s)) > M, result ends with "…"
	properties.Property("If rune length of input > maxLen, result ends with ellipsis", prop.ForAll(
		func(s string, maxLen int) bool {
			runes := []rune(s)

			// Only test when input exceeds maxLen
			if len(runes) <= maxLen {
				return true // skip this case
			}

			result := truncateText(s, maxLen)

			// Check result is valid UTF-8 and ends with the "…" rune
			if !utf8.ValidString(result) {
				t.Logf("Result is not valid UTF-8: %q", result)
				return false
			}

			resultRunes := []rune(result)
			lastRune := resultRunes[len(resultRunes)-1]
			if lastRune != '…' {
				t.Logf("Expected result to end with '…', got last rune: %c", lastRune)
				return false
			}
			return true
		},
		stringGen,
		maxLenGen,
	))

	// Sub-property 4: If len([]rune(s)) > M, the first M runes of result equal the first M runes of the input
	properties.Property("If rune length of input > maxLen, first M runes of result equal first M runes of input", prop.ForAll(
		func(s string, maxLen int) bool {
			runes := []rune(s)

			// Only test when input exceeds maxLen
			if len(runes) <= maxLen {
				return true // skip this case
			}

			result := truncateText(s, maxLen)
			resultRunes := []rune(result)

			// First M runes of result must equal first M runes of input
			if len(resultRunes) < maxLen {
				t.Logf("Result rune length %d is less than maxLen %d", len(resultRunes), maxLen)
				return false
			}

			for i := range maxLen {
				if resultRunes[i] != runes[i] {
					t.Logf("Rune mismatch at position %d: result has %c, input has %c",
						i, resultRunes[i], runes[i])
					return false
				}
			}
			return true
		},
		stringGen,
		maxLenGen,
	))

	properties.TestingRun(t)
}
