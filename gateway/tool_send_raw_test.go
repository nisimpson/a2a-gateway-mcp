package gateway

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// Feature: raw-input-part, Property 1: Raw field round-trip fidelity
// **Validates: Requirements RAW-3.1, RAW-3.4**

func TestPropertyRawRoundTripFidelity(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	properties.Property("any valid byte sequence survives base64 encode → convertInputParts → a2a.Raw", prop.ForAll(
		func(bytes []byte) bool {
			encoded := base64.StdEncoding.EncodeToString(bytes)
			parts := []InputPart{{Raw: &encoded}}

			result, err := convertInputParts(parts)
			if err != nil {
				return false
			}
			if len(result) != 1 {
				return false
			}

			raw, ok := result[0].Content.(a2a.Raw)
			if !ok {
				return false
			}

			// Byte-for-byte comparison
			if len(raw) != len(bytes) {
				return false
			}
			for i := range bytes {
				if raw[i] != bytes[i] {
					return false
				}
			}
			return true
		},
		gen.SliceOf(gen.UInt8()),
	))

	properties.TestingRun(t)
}

// Feature: raw-input-part, Property 2: Exactly-one-field validation includes Raw
// **Validates: Requirements RAW-2.1, RAW-2.2**

func TestPropertyRawExactlyOneFieldValidation(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for a valid base64 string
	validBase64Gen := gen.SliceOf(gen.UInt8()).Map(func(b []byte) string {
		return base64.StdEncoding.EncodeToString(b)
	})

	// Generator for a non-empty text string
	textGen := gen.AnyString().SuchThat(func(s string) bool { return len(s) > 0 })

	properties.Property("raw + text → multiple content types error", prop.ForAll(
		func(rawVal string, textVal string) bool {
			parts := []InputPart{{Raw: &rawVal, Text: &textVal}}
			_, err := convertInputParts(parts)
			return err != nil && strings.Contains(err.Error(), "multiple content types")
		},
		validBase64Gen,
		textGen,
	))

	properties.Property("raw + url → multiple content types error", prop.ForAll(
		func(rawVal string, urlVal string) bool {
			parts := []InputPart{{Raw: &rawVal, URL: &urlVal}}
			_, err := convertInputParts(parts)
			return err != nil && strings.Contains(err.Error(), "multiple content types")
		},
		validBase64Gen,
		gen.AnyString().SuchThat(func(s string) bool { return len(s) > 0 }),
	))

	properties.Property("raw + data → multiple content types error", prop.ForAll(
		func(rawVal string) bool {
			data := map[string]string{"key": "value"}
			parts := []InputPart{{Raw: &rawVal, Data: data}}
			_, err := convertInputParts(parts)
			return err != nil && strings.Contains(err.Error(), "multiple content types")
		},
		validBase64Gen,
	))

	properties.TestingRun(t)
}

// Feature: raw-input-part, Property 3: Invalid base64 rejected with descriptive error
// **Validates: Requirement RAW-3.3**

func TestPropertyRawInvalidBase64Rejected(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for strings that are NOT valid standard base64.
	// We inject characters that are not in the base64 alphabet.
	invalidBase64Gen := gen.AnyString().SuchThat(func(s string) bool {
		// Must actually fail base64 decoding
		_, err := base64.StdEncoding.DecodeString(s)
		return err != nil
	})

	properties.Property("invalid base64 string produces error with index and 'invalid base64'", prop.ForAll(
		func(s string) bool {
			parts := []InputPart{{Raw: &s}}
			_, err := convertInputParts(parts)
			if err == nil {
				return false
			}
			errMsg := err.Error()
			return strings.Contains(errMsg, "index 0") && strings.Contains(errMsg, "invalid base64")
		},
		invalidBase64Gen,
	))

	// Also test at non-zero indices
	properties.Property("invalid base64 at index 1 includes correct index in error", prop.ForAll(
		func(s string) bool {
			validText := "hello"
			parts := []InputPart{
				{Text: &validText},
				{Raw: &s},
			}
			_, err := convertInputParts(parts)
			if err == nil {
				return false
			}
			errMsg := err.Error()
			return strings.Contains(errMsg, "index 1") && strings.Contains(errMsg, "invalid base64")
		},
		invalidBase64Gen,
	))

	properties.TestingRun(t)
}

// Feature: raw-input-part, Property 4: Part ordering preserved with Raw
// **Validates: Requirements RAW-4.1, RAW-4.2**

func TestPropertyRawPartOrderingPreserved(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Type tag for generating mixed parts
	// 0 = text, 1 = raw, 2 = url
	partTypeGen := gen.IntRange(0, 2)

	// Generator for a slice of part types (at least 1 element, with at least one raw)
	partTypesGen := gen.SliceOfN(5, partTypeGen).SuchThat(func(types []int) bool {
		// Ensure at least one raw part
		for _, t := range types {
			if t == 1 {
				return true
			}
		}
		return false
	})

	properties.Property("mixed parts preserve length and type mapping", prop.ForAll(
		func(types []int) bool {
			// Build InputParts from type tags
			parts := make([]InputPart, len(types))
			for i, typ := range types {
				switch typ {
				case 0:
					text := "text-content"
					parts[i] = InputPart{Text: &text}
				case 1:
					raw := base64.StdEncoding.EncodeToString([]byte{0x01, 0x02, 0x03})
					parts[i] = InputPart{Raw: &raw}
				case 2:
					url := "https://example.com"
					parts[i] = InputPart{URL: &url}
				}
			}

			result, err := convertInputParts(parts)
			if err != nil {
				return false
			}

			// Same length
			if len(result) != len(parts) {
				return false
			}

			// Check type correspondence
			for i, typ := range types {
				switch typ {
				case 0:
					if _, ok := result[i].Content.(a2a.Text); !ok {
						return false
					}
				case 1:
					if _, ok := result[i].Content.(a2a.Raw); !ok {
						return false
					}
				case 2:
					if _, ok := result[i].Content.(a2a.URL); !ok {
						return false
					}
				}
			}
			return true
		},
		partTypesGen,
	))

	properties.TestingRun(t)
}

// --- Unit Tests for raw input conversion (Task 4.5) ---
// **Validates: RAW-3.1, RAW-3.2, RAW-3.3, RAW-2.2, RAW-4.1**

func TestRawInput_ValidBase64_DecodedBytes(t *testing.T) {
	input := []byte{0x48, 0x65, 0x6c, 0x6c, 0x6f} // "Hello"
	encoded := base64.StdEncoding.EncodeToString(input)
	parts := []InputPart{{Raw: &encoded}}

	result, err := convertInputParts(parts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 part, got %d", len(result))
	}

	raw, ok := result[0].Content.(a2a.Raw)
	if !ok {
		t.Fatalf("expected a2a.Raw, got %T", result[0].Content)
	}
	if string(raw) != "Hello" {
		t.Errorf("expected %q, got %q", "Hello", string(raw))
	}
}

func TestRawInput_EmptyString_ZeroByteRawPart(t *testing.T) {
	empty := ""
	parts := []InputPart{{Raw: &empty}}

	result, err := convertInputParts(parts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 part, got %d", len(result))
	}

	raw, ok := result[0].Content.(a2a.Raw)
	if !ok {
		t.Fatalf("expected a2a.Raw, got %T", result[0].Content)
	}
	if len(raw) != 0 {
		t.Errorf("expected zero-byte raw, got %d bytes", len(raw))
	}
}

func TestRawInput_InvalidBase64_ErrorWithIndexAndMessage(t *testing.T) {
	invalid := "not-valid-base64!!!"
	parts := []InputPart{{Raw: &invalid}}

	_, err := convertInputParts(parts)
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "index 0") {
		t.Errorf("expected error to contain 'index 0', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "invalid base64") {
		t.Errorf("expected error to contain 'invalid base64', got: %s", errMsg)
	}
}

func TestRawInput_RawCombinedWithText_MultipleContentTypesError(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString([]byte("data"))
	text := "hello"
	parts := []InputPart{{Raw: &raw, Text: &text}}

	_, err := convertInputParts(parts)
	if err == nil {
		t.Fatal("expected error for multiple content types")
	}
	if !strings.Contains(err.Error(), "multiple content types") {
		t.Errorf("expected 'multiple content types' in error, got: %s", err.Error())
	}
}

func TestRawInput_RawCombinedWithData_MultipleContentTypesError(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString([]byte("data"))
	data := map[string]string{"key": "value"}
	parts := []InputPart{{Raw: &raw, Data: data}}

	_, err := convertInputParts(parts)
	if err == nil {
		t.Fatal("expected error for multiple content types")
	}
	if !strings.Contains(err.Error(), "multiple content types") {
		t.Errorf("expected 'multiple content types' in error, got: %s", err.Error())
	}
}

func TestRawInput_RawCombinedWithURL_MultipleContentTypesError(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString([]byte("data"))
	url := "https://example.com"
	parts := []InputPart{{Raw: &raw, URL: &url}}

	_, err := convertInputParts(parts)
	if err == nil {
		t.Fatal("expected error for multiple content types")
	}
	if !strings.Contains(err.Error(), "multiple content types") {
		t.Errorf("expected 'multiple content types' in error, got: %s", err.Error())
	}
}

func TestRawInput_MixedParts_CorrectTypesInOrder(t *testing.T) {
	text := "hello"
	raw := base64.StdEncoding.EncodeToString([]byte{0xFF, 0xFE})
	url := "https://example.com/resource"

	parts := []InputPart{
		{Text: &text},
		{Raw: &raw},
		{URL: &url},
	}

	result, err := convertInputParts(parts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(result))
	}

	// Part 0: text
	if _, ok := result[0].Content.(a2a.Text); !ok {
		t.Errorf("expected part 0 to be a2a.Text, got %T", result[0].Content)
	}

	// Part 1: raw
	rawContent, ok := result[1].Content.(a2a.Raw)
	if !ok {
		t.Errorf("expected part 1 to be a2a.Raw, got %T", result[1].Content)
	} else {
		if len(rawContent) != 2 || rawContent[0] != 0xFF || rawContent[1] != 0xFE {
			t.Errorf("expected raw bytes [0xFF, 0xFE], got %v", rawContent)
		}
	}

	// Part 2: url
	if _, ok := result[2].Content.(a2a.URL); !ok {
		t.Errorf("expected part 2 to be a2a.URL, got %T", result[2].Content)
	}
}
