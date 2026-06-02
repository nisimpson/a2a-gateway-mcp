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

	result := FormatTaskResponse(task)

	if len(result.Content) != 2 {
		t.Fatalf("expected 2 content items, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected first content to be TextContent")
	}
	expected := "Hello World\nSecond artifact"
	if textContent.Text != expected {
		t.Errorf("expected text %q, got %q", expected, textContent.Text)
	}

	ctxContent, ok := result.Content[1].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected second content to be TextContent")
	}
	if ctxContent.Text != "context_id:ctx-123" {
		t.Errorf("expected context_id content %q, got %q", "context_id:ctx-123", ctxContent.Text)
	}
}

func TestFormatTaskResponse_NoArtifacts(t *testing.T) {
	task := &a2a.Task{
		Artifacts: nil,
	}

	result := FormatTaskResponse(task)

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

	result := FormatTaskResponse(task)

	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected content to be TextContent")
	}
	expected := "response contained non-text content that cannot be displayed"
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

	result := FormatTaskResponse(task)

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

	result := FormatTaskResponse(task)

	if len(result.Content) != 2 {
		t.Fatalf("expected 2 content items, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected first content to be TextContent")
	}
	expected := "text part more text"
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

	result := FormatTaskResponse(task)

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

	result := FormatTaskResponse(task)

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

func TestExtractTextFromArtifacts_NilArtifacts(t *testing.T) {
	result, found := extractTextFromArtifacts(nil)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
	if found {
		t.Error("expected found to be false")
	}
}

func TestExtractTextFromArtifacts_NilArtifactEntry(t *testing.T) {
	result, found := extractTextFromArtifacts([]*a2a.Artifact{nil, nil})
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
	if found {
		t.Error("expected found to be false")
	}
}

func TestExtractTextFromArtifacts_MultipleArtifactsSeparatedByNewline(t *testing.T) {
	artifacts := []*a2a.Artifact{
		{Parts: a2a.ContentParts{a2a.NewTextPart("first")}},
		{Parts: a2a.ContentParts{a2a.NewTextPart("second")}},
		{Parts: a2a.ContentParts{a2a.NewTextPart("third")}},
	}

	result, found := extractTextFromArtifacts(artifacts)
	expected := "first\nsecond\nthird"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
	if !found {
		t.Error("expected found to be true")
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

			result := FormatTaskResponse(task)

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

// --- Tests for FormatMessageResponse ---

func TestFormatMessageResponse_WithText(t *testing.T) {
	msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Hello world"))

	result := FormatMessageResponse(msg)

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

	result := FormatMessageResponse(msg)

	if len(result.Content) != 2 {
		t.Fatalf("expected 2 content items, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected first content to be TextContent")
	}
	if textContent.Text != "reply" {
		t.Errorf("expected %q, got %q", "reply", textContent.Text)
	}

	ctxContent, ok := result.Content[1].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected second content to be TextContent")
	}
	if ctxContent.Text != "context_id:ctx-test-1" {
		t.Errorf("expected %q, got %q", "context_id:ctx-test-1", ctxContent.Text)
	}
}

func TestFormatMessageResponse_NonTextParts(t *testing.T) {
	msg := &a2a.Message{
		Role:  a2a.MessageRoleAgent,
		Parts: a2a.ContentParts{a2a.NewDataPart(map[string]any{"key": "value"})},
	}

	result := FormatMessageResponse(msg)

	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	expected := "response contained non-text content that cannot be displayed"
	if textContent.Text != expected {
		t.Errorf("expected %q, got %q", expected, textContent.Text)
	}
}

func TestFormatMessageResponse_NoParts(t *testing.T) {
	msg := &a2a.Message{
		Role:  a2a.MessageRoleAgent,
		Parts: a2a.ContentParts{},
	}

	result := FormatMessageResponse(msg)

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

	result := FormatMessageResponse(msg)

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
