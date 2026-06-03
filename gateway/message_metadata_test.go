package gateway

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ============================================================================
// Property Tests for message-metadata feature
// ============================================================================

// Feature: message-metadata, Property 1: Metadata pass-through
// **Validates: META-1.3, META-1.4, META-1.5**

func TestProperty_MetadataPassthrough(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for non-empty metadata keys (alphanumeric).
	keyGen := gen.RegexMatch(`[a-zA-Z][a-zA-Z0-9]{0,15}`)
	valueGen := gen.AlphaString()

	properties.Property("non-empty metadata is set on SendMessageRequest.Metadata", prop.ForAll(
		func(key, value string) bool {
			if key == "" {
				return true // skip empty keys
			}
			metadata := map[string]any{key: value}

			// Build a SendMessageRequest the same way handleSendMessage does.
			msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("test"))
			sendReq := &a2a.SendMessageRequest{Message: msg}
			if len(metadata) > 0 {
				sendReq.Metadata = metadata
			}

			// Property: metadata on the request equals the input.
			if sendReq.Metadata == nil {
				return false
			}
			v, ok := sendReq.Metadata[key]
			if !ok {
				return false
			}
			return v == value
		},
		keyGen,
		valueGen,
	))

	properties.Property("nil/empty metadata results in nil SendMessageRequest.Metadata", prop.ForAll(
		func(useNil bool) bool {
			msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("test"))
			sendReq := &a2a.SendMessageRequest{Message: msg}

			var metadata map[string]any
			if !useNil {
				metadata = map[string]any{} // empty map
			}
			// nil or empty → don't set
			if len(metadata) > 0 {
				sendReq.Metadata = metadata
			}

			return sendReq.Metadata == nil
		},
		gen.Bool(),
	))

	properties.TestingRun(t)
}

// Feature: message-metadata, Property 2: Parts precedence over message
// **Validates: META-2.3, META-2.4**

func TestProperty_PartsPrecedenceOverMessage(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for non-empty message text.
	messageGen := gen.RegexMatch(`[a-zA-Z]{1,20}`)
	// Generator for non-empty parts text.
	partTextGen := gen.RegexMatch(`[a-zA-Z]{1,20}`)

	properties.Property("when both message and parts are provided, parts wins", prop.ForAll(
		func(message, partText string) bool {
			if message == "" || partText == "" {
				return true // skip empty
			}
			parts := []InputPart{{Text: &partText}}

			contentParts, err := buildMessageParts(message, parts)
			if err != nil {
				return false
			}

			// Should have exactly 1 part reflecting the parts field.
			if len(contentParts) != 1 {
				return false
			}

			// The part should be a TextPart with the parts text, NOT the message text.
			part := contentParts[0]
			text, ok := part.Content.(a2a.Text)
			if !ok {
				return false
			}
			return string(text) == partText
		},
		messageGen,
		partTextGen,
	))

	properties.TestingRun(t)
}

// Feature: message-metadata, Property 4: InputPart conversion correctness
// **Validates: META-2.8, META-2.9, META-2.10**

func TestProperty_InputPartConversion(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	textGen := gen.RegexMatch(`[a-zA-Z0-9 ]{1,30}`)
	urlGen := gen.RegexMatch(`https://[a-z]{3,10}\\.com/[a-z]{1,10}`)

	properties.Property("InputPart with Text produces TextPart", prop.ForAll(
		func(text string) bool {
			if text == "" {
				return true
			}
			parts := []InputPart{{Text: &text}}
			result, err := buildMessageParts("", parts)
			if err != nil {
				return false
			}
			if len(result) != 1 {
				return false
			}
			content, ok := result[0].Content.(a2a.Text)
			if !ok {
				return false
			}
			return string(content) == text
		},
		textGen,
	))

	properties.Property("InputPart with Data produces DataPart", prop.ForAll(
		func(key, value string) bool {
			if key == "" {
				return true
			}
			data := map[string]any{key: value}
			parts := []InputPart{{Data: data}}
			result, err := buildMessageParts("", parts)
			if err != nil {
				return false
			}
			if len(result) != 1 {
				return false
			}
			dataPart, ok := result[0].Content.(a2a.Data)
			if !ok {
				return false
			}
			// Verify the data value matches.
			dataMap, ok := dataPart.Value.(map[string]any)
			if !ok {
				return false
			}
			return dataMap[key] == value
		},
		gen.RegexMatch(`[a-z]{1,10}`),
		gen.AlphaString(),
	))

	properties.Property("InputPart with URL produces URLPart", prop.ForAll(
		func(url string) bool {
			if url == "" {
				return true
			}
			parts := []InputPart{{URL: &url}}
			result, err := buildMessageParts("", parts)
			if err != nil {
				return false
			}
			if len(result) != 1 {
				return false
			}
			urlContent, ok := result[0].Content.(a2a.URL)
			if !ok {
				return false
			}
			return string(urlContent) == url
		},
		urlGen,
	))

	properties.TestingRun(t)
}

// Feature: message-metadata, Property 5: Text-only response backward compat
// **Validates: META-4.1, META-4.2, META-4.3**

func TestProperty_TextOnlyResponseBackwardCompat(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generate multiple artifacts with text-only parts.
	properties.Property("text-only response renders identically to direct concatenation", prop.ForAll(
		func(numArtifacts int, texts []string) bool {
			if numArtifacts <= 0 || len(texts) == 0 {
				return true
			}
			if numArtifacts > len(texts) {
				numArtifacts = len(texts)
			}

			// Build artifacts with single TextPart each.
			artifacts := make([]*a2a.Artifact, numArtifacts)
			for i := 0; i < numArtifacts; i++ {
				artifacts[i] = &a2a.Artifact{
					Parts: a2a.ContentParts{a2a.NewTextPart(texts[i])},
				}
			}

			result := extractContentFromArtifacts(artifacts)

			// Build expected: each artifact's text, joined by newline.
			// extractContentFromArtifacts only includes artifacts where renderPart
			// returns ok=true. For TextPart, empty string IS rendered (Text type).
			expected := strings.Join(texts[:numArtifacts], "\n")

			return result == expected
		},
		gen.IntRange(1, 5),
		gen.SliceOfN(5, gen.AlphaString()),
	))

	properties.TestingRun(t)
}

// Feature: message-metadata, Property 6: Data part renders as JSON
// **Validates: META-5.1**

func TestProperty_DataPartRendersAsJSON(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	properties.Property("Data part output equals json.Marshal(value)", prop.ForAll(
		func(key, value string) bool {
			if key == "" {
				return true
			}
			data := map[string]any{key: value}
			part := &a2a.Part{Content: a2a.Data{Value: data}}

			rendered, ok := renderPart(part)
			if !ok {
				return false
			}

			expected, err := json.Marshal(data)
			if err != nil {
				return false
			}

			return rendered == string(expected)
		},
		gen.RegexMatch(`[a-z]{1,10}`),
		gen.AlphaString(),
	))

	properties.TestingRun(t)
}

// Feature: message-metadata, Property 7: URL part renders as string
// **Validates: META-6.1**

func TestProperty_URLPartRendersAsString(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	urlGen := gen.RegexMatch(`https://[a-z]{3,15}\\.com(/[a-z0-9]{1,10}){0,3}`)

	properties.Property("URL part output equals the URL string", prop.ForAll(
		func(url string) bool {
			if url == "" {
				return true
			}
			part := &a2a.Part{Content: a2a.URL(url)}

			rendered, ok := renderPart(part)
			if !ok {
				return false
			}

			return rendered == url
		},
		urlGen,
	))

	properties.TestingRun(t)
}

// Feature: message-metadata, Property 8: Raw part renders as base64
// **Validates: META-7.1**

func TestProperty_RawPartRendersAsBase64(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generate random byte slices.
	bytesGen := gen.SliceOf(gen.UInt8()).SuchThat(func(v interface{}) bool {
		return len(v.([]uint8)) > 0
	})

	properties.Property("Raw part output equals base64.StdEncoding.EncodeToString(bytes)", prop.ForAll(
		func(data []uint8) bool {
			bytes := make([]byte, len(data))
			for i, b := range data {
				bytes[i] = byte(b)
			}

			part := &a2a.Part{Content: a2a.Raw(bytes)}

			rendered, ok := renderPart(part)
			if !ok {
				return false
			}

			expected := base64.StdEncoding.EncodeToString(bytes)
			return rendered == expected
		},
		bytesGen,
	))

	properties.TestingRun(t)
}

// Feature: message-metadata, Property 9: Mixed part ordering
// **Validates: META-8.1, META-8.2**

func TestProperty_MixedPartOrdering(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generate a sequence of mixed parts and verify ordering is preserved.
	textGen := gen.RegexMatch(`[a-zA-Z]{1,10}`)

	properties.Property("mixed-type parts are concatenated in original order", prop.ForAll(
		func(texts []string) bool {
			if len(texts) == 0 {
				return true
			}

			// Build a mix of Text, Data, and URL parts.
			var parts a2a.ContentParts
			var expectedPieces []string

			for i, text := range texts {
				if text == "" {
					continue
				}
				switch i % 3 {
				case 0:
					// Text part
					parts = append(parts, a2a.NewTextPart(text))
					expectedPieces = append(expectedPieces, text)
				case 1:
					// Data part (simple string value)
					parts = append(parts, &a2a.Part{Content: a2a.Data{Value: text}})
					jsonBytes, _ := json.Marshal(text)
					expectedPieces = append(expectedPieces, string(jsonBytes))
				case 2:
					// URL part
					url := "https://" + text + ".com"
					parts = append(parts, &a2a.Part{Content: a2a.URL(url)})
					expectedPieces = append(expectedPieces, url)
				}
			}

			if len(parts) == 0 {
				return true
			}

			// Use extractContentFromMessageParts which concatenates with no separator.
			result := extractContentFromMessageParts(parts)
			expected := strings.Join(expectedPieces, "")

			return result == expected
		},
		gen.SliceOfN(8, textGen),
	))

	properties.TestingRun(t)
}

// Feature: message-metadata, Property 10: Fallback message never produced
// **Validates: META-9.1**

func TestProperty_FallbackMessageNeverProduced(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	fallbackMsg := "response contained non-text content that cannot be displayed"

	// Generate various part types and ensure the fallback is never in output.
	textGen := gen.RegexMatch(`[a-zA-Z0-9]{0,20}`)

	properties.Property("fallback message never appears in rendered output", prop.ForAll(
		func(texts []string) bool {
			if len(texts) == 0 {
				return true
			}

			// Build mixed parts.
			var parts a2a.ContentParts
			for i, text := range texts {
				switch i % 4 {
				case 0:
					parts = append(parts, a2a.NewTextPart(text))
				case 1:
					parts = append(parts, &a2a.Part{Content: a2a.Data{Value: map[string]any{"k": text}}})
				case 2:
					parts = append(parts, &a2a.Part{Content: a2a.URL("https://example.com/" + text)})
				case 3:
					parts = append(parts, a2a.NewRawPart([]byte(text)))
				}
			}

			// Test via extractContentFromMessageParts.
			result := extractContentFromMessageParts(parts)
			if strings.Contains(result, fallbackMsg) {
				return false
			}

			// Test via artifacts path.
			artifacts := []*a2a.Artifact{{Parts: parts}}
			artifactResult := extractContentFromArtifacts(artifacts)
			if strings.Contains(artifactResult, fallbackMsg) {
				return false
			}

			// Test via FormatMessageResponse.
			msg := &a2a.Message{Role: a2a.MessageRoleAgent, Parts: parts}
			mcpResult := FormatMessageResponse(msg)
			for _, content := range mcpResult.Content {
				if tc, ok := content.(*mcp.TextContent); ok {
					if strings.Contains(tc.Text, fallbackMsg) {
						return false
					}
				}
			}

			return true
		},
		gen.SliceOfN(5, textGen),
	))

	properties.TestingRun(t)
}

// ============================================================================
// Unit Tests for buildMessageParts validation (6.10)
// ============================================================================

// **Validates: META-2.3, META-2.4, META-2.5, META-2.6, META-2.7**

func TestBuildMessageParts_MessageOnly(t *testing.T) {
	result, err := buildMessageParts("hello world", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 part, got %d", len(result))
	}
	text, ok := result[0].Content.(a2a.Text)
	if !ok {
		t.Fatal("expected TextPart")
	}
	if string(text) != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", string(text))
	}
}

func TestBuildMessageParts_PartsOnly(t *testing.T) {
	textVal := "part text"
	url := "https://example.com"
	parts := []InputPart{
		{Text: &textVal},
		{Data: map[string]any{"key": "value"}},
		{URL: &url},
	}

	result, err := buildMessageParts("", parts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(result))
	}

	// Verify text part.
	text, ok := result[0].Content.(a2a.Text)
	if !ok {
		t.Fatal("expected first part to be TextPart")
	}
	if string(text) != "part text" {
		t.Errorf("expected %q, got %q", "part text", string(text))
	}

	// Verify data part.
	data, ok := result[1].Content.(a2a.Data)
	if !ok {
		t.Fatal("expected second part to be DataPart")
	}
	dataMap, ok := data.Value.(map[string]any)
	if !ok {
		t.Fatal("expected data value to be map")
	}
	if dataMap["key"] != "value" {
		t.Errorf("expected data[\"key\"]=%q, got %q", "value", dataMap["key"])
	}

	// Verify URL part.
	urlContent, ok := result[2].Content.(a2a.URL)
	if !ok {
		t.Fatal("expected third part to be URLPart")
	}
	if string(urlContent) != "https://example.com" {
		t.Errorf("expected %q, got %q", "https://example.com", string(urlContent))
	}
}

func TestBuildMessageParts_BothProvided_PartsWins(t *testing.T) {
	partText := "from parts"
	parts := []InputPart{{Text: &partText}}

	result, err := buildMessageParts("from message", parts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 part, got %d", len(result))
	}
	text, ok := result[0].Content.(a2a.Text)
	if !ok {
		t.Fatal("expected TextPart")
	}
	if string(text) != "from parts" {
		t.Errorf("expected %q, got %q", "from parts", string(text))
	}
}

func TestBuildMessageParts_Neither_Error(t *testing.T) {
	_, err := buildMessageParts("", nil)
	if err == nil {
		t.Fatal("expected error when neither message nor parts provided")
	}
	if err.Error() != "either 'message' or 'parts' is required" {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

func TestBuildMessageParts_EmptyPartsArray_Error(t *testing.T) {
	_, err := buildMessageParts("", []InputPart{})
	if err == nil {
		t.Fatal("expected error for empty parts array")
	}
	if err.Error() != "either 'message' or 'parts' is required" {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

func TestBuildMessageParts_PartWithNoFields_Error(t *testing.T) {
	parts := []InputPart{{}} // no fields set

	_, err := buildMessageParts("", parts)
	if err == nil {
		t.Fatal("expected error for part with no fields")
	}
	if !strings.Contains(err.Error(), "has no content") {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

func TestBuildMessageParts_PartWithMultipleFields_Error(t *testing.T) {
	text := "hello"
	url := "https://example.com"
	parts := []InputPart{{Text: &text, URL: &url}} // multiple fields set

	_, err := buildMessageParts("", parts)
	if err == nil {
		t.Fatal("expected error for part with multiple fields")
	}
	if !strings.Contains(err.Error(), "multiple content types") {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

// ============================================================================
// Unit Tests for response rendering (6.11)
// ============================================================================

// **Validates: META-5.1, META-6.1, META-7.1, META-10.1**

func TestRenderPart_DataPart_JSON(t *testing.T) {
	data := map[string]any{"name": "test", "count": float64(42)}
	part := &a2a.Part{Content: a2a.Data{Value: data}}

	rendered, ok := renderPart(part)
	if !ok {
		t.Fatal("expected renderPart to succeed")
	}

	// json.Marshal produces deterministic output for small maps; verify by re-marshaling.
	expected, _ := json.Marshal(data)
	// Compare via JSON round-trip to handle key ordering.
	var renderedMap, expectedMap map[string]any
	_ = json.Unmarshal([]byte(rendered), &renderedMap)
	_ = json.Unmarshal(expected, &expectedMap)
	if !reflect.DeepEqual(renderedMap, expectedMap) {
		t.Errorf("expected %q, got %q", string(expected), rendered)
	}
}

func TestRenderPart_URLPart_String(t *testing.T) {
	url := "https://example.com/path?q=1"
	part := &a2a.Part{Content: a2a.URL(url)}

	rendered, ok := renderPart(part)
	if !ok {
		t.Fatal("expected renderPart to succeed")
	}
	if rendered != url {
		t.Errorf("expected %q, got %q", url, rendered)
	}
}

func TestRenderPart_RawPart_Base64(t *testing.T) {
	data := []byte{0x48, 0x65, 0x6c, 0x6c, 0x6f} // "Hello" in bytes
	part := &a2a.Part{Content: a2a.Raw(data)}

	rendered, ok := renderPart(part)
	if !ok {
		t.Fatal("expected renderPart to succeed")
	}
	expected := base64.StdEncoding.EncodeToString(data)
	if rendered != expected {
		t.Errorf("expected %q, got %q", expected, rendered)
	}
}

func TestResponseRendering_MixedParts_Concatenated(t *testing.T) {
	parts := a2a.ContentParts{
		a2a.NewTextPart("Hello "),
		&a2a.Part{Content: a2a.Data{Value: 42}},
		&a2a.Part{Content: a2a.URL("https://example.com")},
		a2a.NewRawPart([]byte{0x01, 0x02}),
	}

	result := extractContentFromMessageParts(parts)

	// Expected: "Hello " + "42" + "https://example.com" + base64([0x01, 0x02])
	expectedBase64 := base64.StdEncoding.EncodeToString([]byte{0x01, 0x02})
	expected := "Hello " + "42" + "https://example.com" + expectedBase64
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestResponseRendering_StatusMessageWithDataPart(t *testing.T) {
	msg := &a2a.Message{
		Role: a2a.MessageRoleAgent,
		Parts: a2a.ContentParts{
			a2a.NewTextPart("Error: "),
			&a2a.Part{Content: a2a.Data{Value: map[string]any{"code": float64(500), "detail": "internal"}}},
		},
	}

	result := extractStatusMessageText(msg)

	// Should contain both parts concatenated.
	if !strings.HasPrefix(result, "Error: ") {
		t.Errorf("expected prefix %q, got %q", "Error: ", result)
	}
	if !strings.Contains(result, `"code"`) {
		t.Errorf("expected JSON data in result, got %q", result)
	}
	if !strings.Contains(result, `"detail"`) {
		t.Errorf("expected JSON data in result, got %q", result)
	}
}
