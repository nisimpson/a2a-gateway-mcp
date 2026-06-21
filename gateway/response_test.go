package gateway

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestFormatTaskResponse_WithTextArtifacts(t *testing.T) {
	task := &a2a.Task{
		ContextID: "ctx-123",
		Artifacts: []*a2a.Artifact{
			{
				Parts: a2a.ContentParts{
					a2a.NewTextPart("Hello"),
					a2a.NewTextPart(" World"),
				},
			},
			{
				Parts: a2a.ContentParts{
					a2a.NewTextPart("Second artifact"),
				},
			},
		},
	}

	result, _ := FormatTaskResponse(task)

	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected first content to be TextContent")
	}
	expected := "Hello World\nSecond artifact"
	if textContent.Text != expected {
		t.Errorf("expected text %q, got %q", expected, textContent.Text)
	}
}

func TestFormatTaskResponse_NoArtifacts(t *testing.T) {
	task := &a2a.Task{
		Artifacts: nil,
	}

	result, _ := FormatTaskResponse(task)

	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected content to be TextContent")
	}
	if textContent.Text != "" {
		t.Errorf("expected empty text, got %q", textContent.Text)
	}
}

func TestFormatTaskResponse_OnlyNonTextParts(t *testing.T) {
	task := &a2a.Task{
		Artifacts: []*a2a.Artifact{
			{
				Parts: a2a.ContentParts{
					a2a.NewDataPart(map[string]any{"key": "value"}),
				},
			},
		},
	}

	result, _ := FormatTaskResponse(task)

	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected content to be TextContent")
	}
	// Data parts are now rendered as JSON.
	expected := `{"key":"value"}`
	if textContent.Text != expected {
		t.Errorf("expected text %q, got %q", expected, textContent.Text)
	}
}

func TestFormatTaskResponse_EmptyTextParts(t *testing.T) {
	task := &a2a.Task{
		Artifacts: []*a2a.Artifact{
			{
				Parts: a2a.ContentParts{
					a2a.NewTextPart(""),
				},
			},
		},
	}

	result, _ := FormatTaskResponse(task)

	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected content to be TextContent")
	}
	// An empty TextPart is still a TextPart, so we get its (empty) content.
	if textContent.Text != "" {
		t.Errorf("expected empty text, got %q", textContent.Text)
	}
}

func TestFormatTaskResponse_MixedParts(t *testing.T) {
	task := &a2a.Task{
		ContextID: "ctx-456",
		Artifacts: []*a2a.Artifact{
			{
				Parts: a2a.ContentParts{
					a2a.NewTextPart("text part"),
					a2a.NewDataPart(42),
					a2a.NewTextPart(" more text"),
				},
			},
		},
	}

	result, _ := FormatTaskResponse(task)

	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected first content to be TextContent")
	}
	// All parts are now rendered: text + JSON data + text.
	expected := "text part42 more text"
	if textContent.Text != expected {
		t.Errorf("expected text %q, got %q", expected, textContent.Text)
	}
}

func TestFormatTaskResponse_NoContextID(t *testing.T) {
	task := &a2a.Task{
		ContextID: "",
		Artifacts: []*a2a.Artifact{
			{
				Parts: a2a.ContentParts{
					a2a.NewTextPart("hello"),
				},
			},
		},
	}

	result, _ := FormatTaskResponse(task)

	// Should only have 1 content item (no context_id item).
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
}

func TestFormatTaskResponse_ArtifactsWithNoParts(t *testing.T) {
	task := &a2a.Task{
		Artifacts: []*a2a.Artifact{
			{Parts: a2a.ContentParts{}},
			{Parts: nil},
		},
	}

	result, _ := FormatTaskResponse(task)

	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected content to be TextContent")
	}
	if textContent.Text != "" {
		t.Errorf("expected empty text, got %q", textContent.Text)
	}
}

func TestExtractContentFromArtifacts_NilArtifacts(t *testing.T) {
	result := extractContentFromArtifacts(nil)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestExtractContentFromArtifacts_NilArtifactEntry(t *testing.T) {
	result := extractContentFromArtifacts([]*a2a.Artifact{nil, nil})
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestExtractContentFromArtifacts_MultipleArtifactsSeparatedByNewline(t *testing.T) {
	artifacts := []*a2a.Artifact{
		{Parts: a2a.ContentParts{a2a.NewTextPart("first")}},
		{Parts: a2a.ContentParts{a2a.NewTextPart("second")}},
		{Parts: a2a.ContentParts{a2a.NewTextPart("third")}},
	}

	result := extractContentFromArtifacts(artifacts)
	expected := "first\nsecond\nthird"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// Feature: a2a-gateway-mcp, Property 3: Response text extraction preserves content
// **Validates: Requirements AGMCP-2.4, AGMCP-7.1, AGMCP-7.3**

// taskTextInput holds generated test data for the response text extraction property test.
type taskTextInput struct {
	// texts[i] contains the text strings for artifact i
	texts [][]string
}

func TestPropertyResponseTextExtraction(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator that produces a taskTextInput with 1-10 artifacts, each with 1-5 non-empty text parts
	taskTextInputGen := gen.IntRange(1, 10).FlatMap(func(v interface{}) gopter.Gen {
		numArtifacts := v.(int)
		return genTaskTexts(numArtifacts)
	}, reflect.TypeOf(taskTextInput{}))

	properties.Property("FormatTaskResponse concatenates TextPart texts correctly across artifacts", prop.ForAll(
		func(input taskTextInput) bool {
			// Build artifacts with the generated text parts
			artifacts := make([]*a2a.Artifact, len(input.texts))
			for i, artifactTexts := range input.texts {
				parts := make(a2a.ContentParts, len(artifactTexts))
				for j, text := range artifactTexts {
					parts[j] = a2a.NewTextPart(text)
				}
				artifacts[i] = &a2a.Artifact{Parts: parts}
			}

			task := &a2a.Task{
				Artifacts: artifacts,
			}

			result, _ := FormatTaskResponse(task)

			// Verify at least 1 content item
			if len(result.Content) < 1 {
				return false
			}

			// Verify first content item is TextContent
			textContent, ok := result.Content[0].(*mcp.TextContent)
			if !ok {
				return false
			}

			// Build expected concatenation:
			// Within each artifact, TextPart texts are joined directly (no separator)
			// Between artifacts, texts are joined with "\n"
			artifactStrs := make([]string, len(input.texts))
			for i, artifactTexts := range input.texts {
				combined := ""
				for _, text := range artifactTexts {
					combined += text
				}
				artifactStrs[i] = combined
			}

			expected := ""
			for i, at := range artifactStrs {
				if i > 0 {
					expected += "\n"
				}
				expected += at
			}

			return textContent.Text == expected
		},
		taskTextInputGen,
	))

	properties.TestingRun(t)
}

// genTaskTexts generates a taskTextInput with the given number of artifacts,
// each having 1-5 non-empty alpha text parts.
func genTaskTexts(numArtifacts int) gopter.Gen {
	return func(params *gopter.GenParameters) *gopter.GenResult {
		texts := make([][]string, numArtifacts)
		nonEmptyAlpha := gen.AlphaString().SuchThat(func(v interface{}) bool {
			return len(v.(string)) > 0
		})

		for i := 0; i < numArtifacts; i++ {
			// Generate number of parts for this artifact (1-5)
			numPartsResult := gen.IntRange(1, 5)(params)
			numPartsVal, ok := numPartsResult.Retrieve()
			if !ok {
				return gopter.NewEmptyResult(reflect.TypeOf(taskTextInput{}))
			}
			numParts := numPartsVal.(int)

			// Generate text for each part
			parts := make([]string, numParts)
			for j := 0; j < numParts; j++ {
				textResult := nonEmptyAlpha(params)
				textVal, ok := textResult.Retrieve()
				if !ok {
					return gopter.NewEmptyResult(reflect.TypeOf(taskTextInput{}))
				}
				parts[j] = textVal.(string)
			}
			texts[i] = parts
		}

		return gopter.NewGenResult(taskTextInput{texts: texts}, gopter.NoShrinker)
	}
}

// Feature: a2a-gateway-mcp, Property 4: A2A response JSON round-trip
// **Validates: Requirements AGMCP-8.3**

func TestPropertyA2AResponseJSONRoundTrip(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for non-empty strings (used for ContextID)
	nonEmptyStringGen := gen.RegexMatch(`[a-zA-Z0-9\-]{1,32}`)

	// Generator for text content
	textGen := gen.RegexMatch(`[a-zA-Z0-9 ]{0,64}`)

	// Generator for a single TextPart
	textPartGen := textGen.Map(func(text string) *a2a.Part {
		return a2a.NewTextPart(text)
	})

	// Generator for an artifact with random TextParts (1-5 parts)
	artifactGen := gen.SliceOfN(5, textPartGen).Map(func(parts []*a2a.Part) *a2a.Artifact {
		if len(parts) == 0 {
			parts = []*a2a.Part{a2a.NewTextPart("default")}
		}
		return &a2a.Artifact{
			ID:    a2a.NewArtifactID(),
			Parts: a2a.ContentParts(parts),
		}
	})

	// Generator for number of artifacts (0-5)
	numArtifactsGen := gen.IntRange(0, 5)

	properties.Property("serialize → deserialize → re-serialize produces equivalent JSON", prop.ForAll(
		func(contextID string, numArtifacts int, artifacts []*a2a.Artifact) bool {
			// Trim artifacts to the desired count
			if numArtifacts < len(artifacts) {
				artifacts = artifacts[:numArtifacts]
			}

			// Build the Task
			task := &a2a.Task{
				ID:        a2a.NewTaskID(),
				ContextID: contextID,
				Artifacts: artifacts,
				Status: a2a.TaskStatus{
					State: a2a.TaskStateCompleted,
				},
			}

			// Step 1: Serialize to JSON
			jsonBytes1, err := json.Marshal(task)
			if err != nil {
				return false
			}

			// Step 2: Deserialize into a new Task struct
			var task2 a2a.Task
			if err := json.Unmarshal(jsonBytes1, &task2); err != nil {
				return false
			}

			// Step 3: Re-serialize the new struct
			jsonBytes2, err := json.Marshal(&task2)
			if err != nil {
				return false
			}

			// Step 4: Compare by unmarshaling both into map[string]interface{} and comparing
			var map1, map2 map[string]interface{}
			if err := json.Unmarshal(jsonBytes1, &map1); err != nil {
				return false
			}
			if err := json.Unmarshal(jsonBytes2, &map2); err != nil {
				return false
			}

			return reflect.DeepEqual(map1, map2)
		},
		nonEmptyStringGen,
		numArtifactsGen,
		gen.SliceOfN(5, artifactGen),
	))

	properties.TestingRun(t)
}

// Feature: structured-responses, Property 2: Content only contains response text
// **Validates: Requirements SRES-1.4, SRES-2.3**

// referenceFormatTaskResponse is the reference implementation for FormatTaskResponse.
// It computes the expected content items: only the response text.
func referenceFormatTaskResponse(task *a2a.Task) []*mcp.TextContent {
	var items []*mcp.TextContent

	text := extractContentFromArtifacts(task.Artifacts)
	items = append(items, &mcp.TextContent{Text: text})

	return items
}

// referenceFormatMessageResponse is the reference implementation for FormatMessageResponse.
// It computes the expected content items: only the response text.
func referenceFormatMessageResponse(msg *a2a.Message) []*mcp.TextContent {
	var items []*mcp.TextContent

	text := extractContentFromMessageParts(msg.Parts)
	items = append(items, &mcp.TextContent{Text: text})

	return items
}

// referenceFormatInterruptedResponse is the reference implementation for FormatInterruptedResponse.
// It computes the expected content items: response text + state indicator.
func referenceFormatInterruptedResponse(task *a2a.Task, stateName string) []*mcp.TextContent {
	var items []*mcp.TextContent

	var responseText string
	if task.Status.Message != nil {
		responseText = extractStatusMessageText(task.Status.Message)
	}

	if responseText == "" {
		responseText = extractContentFromArtifacts(task.Artifacts)
	}

	if responseText != "" {
		items = append(items, &mcp.TextContent{Text: responseText})
	}

	items = append(items, &mcp.TextContent{Text: "state:" + stateName})

	return items
}

// contentItemsEqual compares two slices of content items for equality in count, order, and content.
func contentItemsEqual(actual []mcp.Content, expected []*mcp.TextContent) bool {
	if len(actual) != len(expected) {
		return false
	}
	for i := range actual {
		tc, ok := actual[i].(*mcp.TextContent)
		if !ok {
			return false
		}
		if tc.Text != expected[i].Text {
			return false
		}
	}
	return true
}

// genTaskID generates optional task IDs (empty string or a random ID).
func genTaskID() gopter.Gen {
	return gen.OneGenOf(
		gen.Const(""),
		gen.RegexMatch(`[a-zA-Z0-9\-]{1,32}`),
	)
}

// genContextID generates optional context IDs (empty string or a random ID).
func genContextID() gopter.Gen {
	return gen.OneGenOf(
		gen.Const(""),
		gen.RegexMatch(`[a-zA-Z0-9\-]{1,32}`),
	)
}

// genTextPart generates a random text part.
func genTextPart() gopter.Gen {
	return gen.RegexMatch(`[a-zA-Z0-9 ]{0,32}`).Map(func(text string) *a2a.Part {
		return a2a.NewTextPart(text)
	})
}

// genDataPart generates a random data part.
func genDataPart() gopter.Gen {
	return gen.RegexMatch(`[a-zA-Z]{1,8}`).Map(func(key string) *a2a.Part {
		return a2a.NewDataPart(map[string]any{"key": key})
	})
}

// genPart generates a random part (text or data).
func genPart() gopter.Gen {
	return gen.OneGenOf(genTextPart(), genDataPart())
}

// genContentParts generates a slice of 0-5 random parts.
func genContentParts() gopter.Gen {
	return gen.SliceOfN(5, genPart())
}

// genArtifact generates a random artifact with 0-5 parts.
func genArtifact() gopter.Gen {
	return genContentParts().Map(func(parts []*a2a.Part) *a2a.Artifact {
		return &a2a.Artifact{Parts: a2a.ContentParts(parts)}
	})
}

// genArtifacts generates a slice of 0-3 random artifacts.
func genArtifacts() gopter.Gen {
	return gen.SliceOfN(3, genArtifact())
}

// genTaskForLegacy generates a random *a2a.Task for legacy content testing.
func genTaskForLegacy() gopter.Gen {
	return gopter.CombineGens(
		genTaskID(),
		genContextID(),
		genArtifacts(),
	).Map(func(values []interface{}) *a2a.Task {
		return &a2a.Task{
			ID:        a2a.TaskID(values[0].(string)),
			ContextID: values[1].(string),
			Artifacts: values[2].([]*a2a.Artifact),
			Status: a2a.TaskStatus{
				State: a2a.TaskStateCompleted,
			},
		}
	})
}

// genMessageForLegacy generates a random *a2a.Message for legacy content testing.
func genMessageForLegacy() gopter.Gen {
	return gopter.CombineGens(
		genContextID(),
		genContentParts(),
	).Map(func(values []interface{}) *a2a.Message {
		return &a2a.Message{
			Role:      a2a.MessageRoleAgent,
			ContextID: values[0].(string),
			Parts:     a2a.ContentParts(values[1].([]*a2a.Part)),
		}
	})
}

// genInterruptedTask generates a random *a2a.Task with a status message for interrupted testing.
func genInterruptedTask() gopter.Gen {
	return gopter.CombineGens(
		genTaskID(),
		genContextID(),
		genArtifacts(),
		gen.OneGenOf(
			gen.Const(""),
			gen.RegexMatch(`[a-zA-Z0-9 ]{1,32}`),
		),
	).Map(func(values []interface{}) *a2a.Task {
		task := &a2a.Task{
			ID:        a2a.TaskID(values[0].(string)),
			ContextID: values[1].(string),
			Artifacts: values[2].([]*a2a.Artifact),
			Status: a2a.TaskStatus{
				State: a2a.TaskStateInputRequired,
			},
		}
		statusText := values[3].(string)
		if statusText != "" {
			task.Status.Message = a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(statusText))
		}
		return task
	})
}

func TestPropertyContentOnlyContainsResponseText(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Property 2a: FormatTaskResponse content matches reference
	properties.Property("FormatTaskResponse content only contains response text", prop.ForAll(
		func(task *a2a.Task) bool {
			result, _ := FormatTaskResponse(task)
			expected := referenceFormatTaskResponse(task)
			return contentItemsEqual(result.Content, expected)
		},
		genTaskForLegacy(),
	))

	// Property 2b: FormatMessageResponse content matches reference
	properties.Property("FormatMessageResponse content only contains response text", prop.ForAll(
		func(msg *a2a.Message) bool {
			result, _ := FormatMessageResponse(msg)
			expected := referenceFormatMessageResponse(msg)
			return contentItemsEqual(result.Content, expected)
		},
		genMessageForLegacy(),
	))

	// Property 2c: FormatInterruptedResponse content matches reference
	properties.Property("FormatInterruptedResponse content only contains response text and state", prop.ForAll(
		func(task *a2a.Task) bool {
			result, _ := FormatInterruptedResponse(task, "input-required")
			expected := referenceFormatInterruptedResponse(task, "input-required")
			return contentItemsEqual(result.Content, expected)
		},
		genInterruptedTask(),
	))

	// Property 2d: FormatInputRequiredResponse content matches reference (delegates to FormatInterruptedResponse)
	properties.Property("FormatInputRequiredResponse content only contains response text and state", prop.ForAll(
		func(task *a2a.Task) bool {
			result, _ := FormatInputRequiredResponse(task)
			expected := referenceFormatInterruptedResponse(task, "input-required")
			return contentItemsEqual(result.Content, expected)
		},
		genInterruptedTask(),
	))

	properties.TestingRun(t)
}

// Feature: structured-responses, Property 4: JSON uses camelCase and omitempty
// **Validates: Requirements SRES-1.2, SRES-6.1**

func TestPropertySerializationCorrectness(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generators for building randomized Task objects
	nonEmptyStringGen := gen.RegexMatch(`[a-zA-Z0-9\-]{1,32}`)
	textGen := gen.RegexMatch(`[a-zA-Z0-9 ]{1,64}`)

	textPartGen := textGen.Map(func(text string) *a2a.Part {
		return a2a.NewTextPart(text)
	})

	artifactGen := gen.SliceOfN(3, textPartGen).Map(func(parts []*a2a.Part) *a2a.Artifact {
		if len(parts) == 0 {
			parts = []*a2a.Part{a2a.NewTextPart("default")}
		}
		return &a2a.Artifact{
			ID:    a2a.NewArtifactID(),
			Parts: a2a.ContentParts(parts),
		}
	})

	taskStateGen := gen.OneConstOf(
		a2a.TaskStateCompleted,
		a2a.TaskStateFailed,
		a2a.TaskStateSubmitted,
		a2a.TaskStateWorking,
		a2a.TaskStateInputRequired,
	)

	properties.Property("structured content JSON uses camelCase keys and omits empty fields", prop.ForAll(
		func(contextID string, state a2a.TaskState, artifacts []*a2a.Artifact, includeMetadata bool) bool {
			// Build a Task with varying populated fields
			task := &a2a.Task{
				ID:        a2a.NewTaskID(),
				ContextID: contextID,
				Status:    a2a.TaskStatus{State: state},
				Artifacts: artifacts,
			}

			// Optionally add metadata
			if includeMetadata {
				task.Metadata = map[string]any{"testKey": "testValue"}
			}

			// Get the structured content from FormatTaskResponse
			_, structured := FormatTaskResponse(task)

			// Marshal to JSON
			jsonBytes, err := json.Marshal(structured)
			if err != nil {
				t.Logf("marshal error: %v", err)
				return false
			}

			// Parse into a generic map to inspect the SendMessageResponse envelope
			var rawMap map[string]any
			if err := json.Unmarshal(jsonBytes, &rawMap); err != nil {
				t.Logf("unmarshal error: %v", err)
				return false
			}

			// The top-level envelope should have a "task" key
			taskField, exists := rawMap["task"]
			if !exists {
				t.Logf("expected 'task' key in SendMessageResponse envelope")
				return false
			}
			taskMap, ok := taskField.(map[string]any)
			if !ok {
				t.Logf("expected 'task' to be an object")
				return false
			}

			// Property 4a: Verify camelCase field names in the task object
			if !allKeysCamelCase(taskMap) {
				t.Logf("found non-camelCase key in task: %s", string(jsonBytes))
				return false
			}

			// Property 4b: Verify omitempty — nil/empty fields should be absent
			if len(task.History) == 0 {
				if _, exists := taskMap["history"]; exists {
					t.Logf("expected 'history' to be omitted when nil/empty")
					return false
				}
			}

			if task.Metadata == nil {
				if _, exists := taskMap["metadata"]; exists {
					t.Logf("expected 'metadata' to be omitted when nil")
					return false
				}
			}

			if len(task.Artifacts) == 0 {
				if _, exists := taskMap["artifacts"]; exists {
					t.Logf("expected 'artifacts' to be omitted when nil/empty")
					return false
				}
			}

			return true
		},
		nonEmptyStringGen,
		taskStateGen,
		gen.SliceOfN(5, artifactGen),
		gen.Bool(),
	))

	properties.TestingRun(t)
}

// allKeysCamelCase recursively checks that all JSON object keys start with a
// lowercase letter (camelCase convention). It inspects nested maps and arrays.
func allKeysCamelCase(m map[string]any) bool {
	for key, val := range m {
		// All keys must start with a lowercase letter
		if len(key) > 0 && key[0] >= 'A' && key[0] <= 'Z' {
			return false
		}
		// Recursively check nested objects
		switch v := val.(type) {
		case map[string]any:
			if !allKeysCamelCase(v) {
				return false
			}
		case []any:
			for _, item := range v {
				if nestedMap, ok := item.(map[string]any); ok {
					if !allKeysCamelCase(nestedMap) {
						return false
					}
				}
			}
		}
	}
	return true
}

// --- Tests for FormatMessageResponse ---

func TestFormatMessageResponse_WithText(t *testing.T) {
	msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Hello world"))

	result, _ := FormatMessageResponse(msg)

	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text != "Hello world" {
		t.Errorf("expected %q, got %q", "Hello world", textContent.Text)
	}
}

func TestFormatMessageResponse_WithContextID(t *testing.T) {
	msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("reply"))
	msg.ContextID = "ctx-test-1"

	result, structured := FormatMessageResponse(msg)

	// Content should only have the response text (no context_id item).
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected first content to be TextContent")
	}
	if textContent.Text != "reply" {
		t.Errorf("expected %q, got %q", "reply", textContent.Text)
	}

	// Context ID should be available via structured response.
	if structured.Message.ContextID != "ctx-test-1" {
		t.Errorf("expected structured context_id %q, got %q", "ctx-test-1", structured.Message.ContextID)
	}
}

func TestFormatMessageResponse_NonTextParts(t *testing.T) {
	msg := &a2a.Message{
		Role:  a2a.MessageRoleAgent,
		Parts: a2a.ContentParts{a2a.NewDataPart(map[string]any{"key": "value"})},
	}

	result, _ := FormatMessageResponse(msg)

	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	// Data parts are now rendered as JSON.
	expected := `{"key":"value"}`
	if textContent.Text != expected {
		t.Errorf("expected %q, got %q", expected, textContent.Text)
	}
}

func TestFormatMessageResponse_NoParts(t *testing.T) {
	msg := &a2a.Message{
		Role:  a2a.MessageRoleAgent,
		Parts: a2a.ContentParts{},
	}

	result, _ := FormatMessageResponse(msg)

	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text != "" {
		t.Errorf("expected empty text, got %q", textContent.Text)
	}
}

func TestFormatMessageResponse_MultipleTextParts(t *testing.T) {
	msg := &a2a.Message{
		Role: a2a.MessageRoleAgent,
		Parts: a2a.ContentParts{
			a2a.NewTextPart("Hello "),
			a2a.NewTextPart("World"),
		},
	}

	result, _ := FormatMessageResponse(msg)

	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text != "Hello World" {
		t.Errorf("expected %q, got %q", "Hello World", textContent.Text)
	}
}

// Feature: structured-responses, Property 1: Structured content is the same pointer as input
// **Validates: Requirements SRES-1.1, SRES-1.3, SRES-2.1, SRES-2.2, SRES-3.1, SRES-3.2, SRES-6.3**

func TestPropertyStructuredContentIdentity(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for task states
	taskStateGen := gen.OneConstOf(
		a2a.TaskStateCompleted,
		a2a.TaskStateFailed,
		a2a.TaskStateCanceled,
		a2a.TaskStateInputRequired,
		a2a.TaskStateAuthRequired,
		a2a.TaskStateSubmitted,
		a2a.TaskStateWorking,
	)

	// Generator for message roles
	roleGen := gen.OneConstOf(
		a2a.MessageRoleAgent,
		a2a.MessageRoleUser,
	)

	// Generator for text content parts
	textPartGen := gen.AlphaString().Map(func(s string) *a2a.Part {
		return a2a.NewTextPart(s)
	})

	// Generator for a slice of content parts (1-5 parts)
	partsGen := gen.SliceOfN(5, textPartGen).SuchThat(func(v interface{}) bool {
		return len(v.([]*a2a.Part)) > 0
	})

	// Generator for an artifact
	artifactGen := partsGen.Map(func(parts []*a2a.Part) *a2a.Artifact {
		return &a2a.Artifact{
			ID:    a2a.NewArtifactID(),
			Parts: a2a.ContentParts(parts),
		}
	})

	// Generator for a slice of artifacts (0-3)
	artifactsGen := gen.SliceOfN(3, artifactGen)

	// Generator for a *a2a.Task
	taskGen := gopter.CombineGens(
		gen.RegexMatch(`[a-zA-Z0-9\-]{1,32}`), // contextID
		taskStateGen,
		artifactsGen,
	).Map(func(values []interface{}) *a2a.Task {
		contextID := values[0].(string)
		state := values[1].(a2a.TaskState)
		artifacts := values[2].([]*a2a.Artifact)
		return &a2a.Task{
			ID:        a2a.NewTaskID(),
			ContextID: contextID,
			Status:    a2a.TaskStatus{State: state},
			Artifacts: artifacts,
		}
	})

	// Generator for a *a2a.Message
	messageGen := gopter.CombineGens(
		gen.RegexMatch(`[a-zA-Z0-9\-]{0,32}`), // contextID
		roleGen,
		partsGen,
	).Map(func(values []interface{}) *a2a.Message {
		contextID := values[0].(string)
		role := values[1].(a2a.MessageRole)
		parts := values[2].([]*a2a.Part)
		return &a2a.Message{
			ID:        a2a.NewMessageID(),
			ContextID: contextID,
			Role:      role,
			Parts:     a2a.ContentParts(parts),
		}
	})

	// Property: FormatTaskResponse returns a SendMessageResponse wrapping the exact input task pointer
	properties.Property("FormatTaskResponse returns SendMessageResponse with exact task pointer", prop.ForAll(
		func(task *a2a.Task) bool {
			_, resp := FormatTaskResponse(task)
			return resp != nil && resp.Task == task && resp.Message == nil
		},
		taskGen,
	))

	// Property: FormatMessageResponse returns a SendMessageResponse wrapping the exact input message pointer
	properties.Property("FormatMessageResponse returns SendMessageResponse with exact message pointer", prop.ForAll(
		func(msg *a2a.Message) bool {
			_, resp := FormatMessageResponse(msg)
			return resp != nil && resp.Message == msg && resp.Task == nil
		},
		messageGen,
	))

	// Property: FormatInterruptedResponse returns a SendMessageResponse wrapping the exact input task pointer
	properties.Property("FormatInterruptedResponse returns SendMessageResponse with exact task pointer", prop.ForAll(
		func(task *a2a.Task) bool {
			_, resp := FormatInterruptedResponse(task, "input-required")
			return resp != nil && resp.Task == task && resp.Message == nil
		},
		taskGen,
	))

	// Property: FormatInputRequiredResponse returns a SendMessageResponse wrapping the exact input task pointer
	properties.Property("FormatInputRequiredResponse returns SendMessageResponse with exact task pointer", prop.ForAll(
		func(task *a2a.Task) bool {
			_, resp := FormatInputRequiredResponse(task)
			return resp != nil && resp.Task == task && resp.Message == nil
		},
		taskGen,
	))

	properties.TestingRun(t)
}
