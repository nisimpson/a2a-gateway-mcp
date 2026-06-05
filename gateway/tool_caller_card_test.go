package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ============================================================================
// Property-Based Tests for caller-agent-card feature
// ============================================================================

// Feature: caller-agent-card, Property 1: Whitespace name rejection
// **Validates: Requirements CAC-1.8**

func TestProperty_WhitespaceNameRejection(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for whitespace-only strings (spaces, tabs, newlines).
	whitespaceGen := gen.RegexMatch(`[\s]{0,20}`)

	properties.Property("whitespace-only name returns error and no card is stored", prop.ForAll(
		func(name string) bool {
			// Only test strings that are truly empty/whitespace-only.
			if strings.TrimSpace(name) != "" {
				return true // skip non-whitespace strings
			}

			srv := NewServer()

			input := CreateCallerCardInput{
				Name:        name,
				Description: "test description",
			}

			result, _, err := srv.handleCreateCallerCard(context.Background(), nil, input)
			if err != nil {
				return false
			}

			// Must be an error result.
			if !result.IsError {
				return false
			}

			// No card should be stored.
			srv.callerCardMu.RLock()
			card := srv.callerCard
			srv.callerCardMu.RUnlock()

			return card == nil
		},
		whitespaceGen,
	))

	properties.TestingRun(t)
}

// Feature: caller-agent-card, Property 2: Create/Get round-trip
// **Validates: Requirements CAC-1.9, CAC-3.1, CAC-3.3**

func TestProperty_CreateGetRoundTrip(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generators for valid card fields.
	nameGen := gen.RegexMatch(`[a-zA-Z][a-zA-Z0-9 ]{0,19}`)
	descGen := gen.AlphaString()
	urlGen := gen.RegexMatch(`https://[a-z]{3,10}\.[a-z]{2,4}`)

	properties.Property("view_caller_card returns what was created", prop.ForAll(
		func(name, desc, url string) bool {
			if strings.TrimSpace(name) == "" {
				return true // skip invalid names
			}

			srv := NewServer()

			input := CreateCallerCardInput{
				Name:        name,
				Description: desc,
				URL:         url,
			}

			createResult, _, err := srv.handleCreateCallerCard(context.Background(), nil, input)
			if err != nil || createResult.IsError {
				return false
			}

			// View the card.
			viewResult, _, err := srv.handleViewCallerCard(context.Background(), nil, ViewCallerCardInput{})
			if err != nil || viewResult.IsError {
				return false
			}

			// Parse the JSON output.
			textContent, ok := viewResult.Content[0].(*mcp.TextContent)
			if !ok {
				return false
			}

			var card CallerCard
			if err := json.Unmarshal([]byte(textContent.Text), &card); err != nil {
				return false
			}

			return card.Name == name && card.Description == desc && card.URL == url
		},
		nameGen,
		descGen,
		urlGen,
	))

	properties.TestingRun(t)
}

// Feature: caller-agent-card, Property 3: Create replaces existing card
// **Validates: Requirements CAC-1.10**

func TestProperty_CreateReplacesExisting(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	nameGen := gen.RegexMatch(`[a-zA-Z][a-zA-Z0-9]{0,9}`)
	descGen := gen.AlphaString()

	properties.Property("second create replaces first card entirely", prop.ForAll(
		func(nameA, descA, nameB, descB string) bool {
			if strings.TrimSpace(nameA) == "" || strings.TrimSpace(nameB) == "" {
				return true
			}

			srv := NewServer()

			// Create first card.
			inputA := CreateCallerCardInput{Name: nameA, Description: descA}
			resultA, _, err := srv.handleCreateCallerCard(context.Background(), nil, inputA)
			if err != nil || resultA.IsError {
				return false
			}

			// Create second card (replacement).
			inputB := CreateCallerCardInput{Name: nameB, Description: descB}
			resultB, _, err := srv.handleCreateCallerCard(context.Background(), nil, inputB)
			if err != nil || resultB.IsError {
				return false
			}

			// Stored card should match B.
			srv.callerCardMu.RLock()
			card := srv.callerCard
			srv.callerCardMu.RUnlock()

			if card == nil {
				return false
			}

			return card.Name == nameB && card.Description == descB
		},
		nameGen,
		descGen,
		nameGen,
		descGen,
	))

	properties.TestingRun(t)
}

// Feature: caller-agent-card, Property 4: Card injection into metadata
// **Validates: Requirements CAC-2.1, CAC-2.2, CAC-2.4**

func TestProperty_CardInjection(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	nameGen := gen.RegexMatch(`[a-zA-Z][a-zA-Z0-9]{0,9}`)
	descGen := gen.AlphaString()
	keyGen := gen.RegexMatch(`[a-z]{1,10}`)
	valueGen := gen.AlphaString()

	properties.Property("injection adds card alongside existing metadata entries", prop.ForAll(
		func(name, desc, metaKey, metaValue string) bool {
			if strings.TrimSpace(name) == "" || metaKey == "" {
				return true
			}

			// Don't let the metadata key conflict with the default card key.
			if metaKey == defaultCallerCardKey {
				return true
			}

			srv := NewServer()

			// Register a card.
			input := CreateCallerCardInput{Name: name, Description: desc}
			_, _, _ = srv.handleCreateCallerCard(context.Background(), nil, input)

			// Create metadata without the configured key.
			metadata := map[string]any{metaKey: metaValue}

			// Inject.
			result := srv.injectCallerCard(metadata)

			// Card should be present under the default key.
			_, hasCard := result[defaultCallerCardKey]
			if !hasCard {
				return false
			}

			// Original entry should still be present.
			v, hasOriginal := result[metaKey]
			if !hasOriginal || v != metaValue {
				return false
			}

			return true
		},
		nameGen,
		descGen,
		keyGen,
		valueGen,
	))

	properties.TestingRun(t)
}

// Feature: caller-agent-card, Property 5: No injection when no card registered
// **Validates: Requirements CAC-2.3**

func TestProperty_NoInjectionWithoutCard(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	keyGen := gen.RegexMatch(`[a-z]{1,10}`)
	valueGen := gen.AlphaString()

	properties.Property("injectCallerCard returns metadata unchanged when no card", prop.ForAll(
		func(key, value string) bool {
			if key == "" {
				return true
			}

			srv := NewServer() // no card registered

			metadata := map[string]any{key: value}
			result := srv.injectCallerCard(metadata)

			// No new keys should be added.
			if len(result) != len(metadata) {
				return false
			}

			// The caller_agent_card key should NOT exist.
			_, hasCard := result[defaultCallerCardKey]
			return !hasCard
		},
		keyGen,
		valueGen,
	))

	properties.Property("injectCallerCard returns nil metadata unchanged when no card", prop.ForAll(
		func(dummy bool) bool {
			srv := NewServer() // no card registered

			result := srv.injectCallerCard(nil)
			return result == nil
		},
		gen.Bool(),
	))

	properties.TestingRun(t)
}

// Feature: caller-agent-card, Property 6: User metadata key precedence
// **Validates: Requirements CAC-2.5**

func TestProperty_UserMetadataKeyPrecedence(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	nameGen := gen.RegexMatch(`[a-zA-Z][a-zA-Z0-9]{0,9}`)
	descGen := gen.AlphaString()
	userValueGen := gen.AlphaString()

	properties.Property("user-provided value for the card key is preserved", prop.ForAll(
		func(name, desc, userValue string) bool {
			if strings.TrimSpace(name) == "" {
				return true
			}

			srv := NewServer()

			// Register a card.
			input := CreateCallerCardInput{Name: name, Description: desc}
			_, _, _ = srv.handleCreateCallerCard(context.Background(), nil, input)

			// Create metadata with the same key as the card key.
			metadata := map[string]any{defaultCallerCardKey: userValue}

			// Inject — should NOT overwrite.
			result := srv.injectCallerCard(metadata)

			// User value should be preserved.
			v, exists := result[defaultCallerCardKey]
			if !exists {
				return false
			}

			return v == userValue
		},
		nameGen,
		descGen,
		userValueGen,
	))

	properties.Property("custom metadata key precedence is also preserved", prop.ForAll(
		func(name, desc, customKey, userValue string) bool {
			if strings.TrimSpace(name) == "" || customKey == "" {
				return true
			}

			srv := NewServer()

			// Register a card with custom key.
			input := CreateCallerCardInput{Name: name, Description: desc, MetadataKey: customKey}
			_, _, _ = srv.handleCreateCallerCard(context.Background(), nil, input)

			// Create metadata with the same custom key.
			metadata := map[string]any{customKey: userValue}

			// Inject — should NOT overwrite.
			result := srv.injectCallerCard(metadata)

			// User value should be preserved.
			v, exists := result[customKey]
			if !exists {
				return false
			}

			return v == userValue
		},
		nameGen,
		descGen,
		gen.RegexMatch(`[a-z]{1,10}`),
		userValueGen,
	))

	properties.TestingRun(t)
}

// Feature: caller-agent-card, Property 7: Remove clears stored card
// **Validates: Requirements CAC-3.4, CAC-3.5**

func TestProperty_RemoveClearsCard(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	nameGen := gen.RegexMatch(`[a-zA-Z][a-zA-Z0-9]{0,9}`)
	descGen := gen.AlphaString()

	properties.Property("after remove, card is nil and injection does nothing", prop.ForAll(
		func(name, desc string) bool {
			if strings.TrimSpace(name) == "" {
				return true
			}

			srv := NewServer()

			// Register a card.
			input := CreateCallerCardInput{Name: name, Description: desc}
			createResult, _, err := srv.handleCreateCallerCard(context.Background(), nil, input)
			if err != nil || createResult.IsError {
				return false
			}

			// Remove the card.
			removeResult, _, err := srv.handleRemoveCallerCard(context.Background(), nil, RemoveCallerCardInput{})
			if err != nil || removeResult.IsError {
				return false
			}

			// Card should be nil.
			srv.callerCardMu.RLock()
			card := srv.callerCard
			srv.callerCardMu.RUnlock()

			if card != nil {
				return false
			}

			// Injection should not add anything.
			metadata := map[string]any{"existing": "value"}
			result := srv.injectCallerCard(metadata)

			_, hasCard := result[defaultCallerCardKey]
			return !hasCard && len(result) == 1
		},
		nameGen,
		descGen,
	))

	properties.TestingRun(t)
}

// Feature: caller-agent-card, Property 8: JSON serialization field compatibility
// **Validates: Requirements CAC-4.1, CAC-4.2, CAC-4.4**

func TestProperty_SerializationFieldNames(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	nameGen := gen.RegexMatch(`[a-zA-Z][a-zA-Z0-9]{0,9}`)
	descGen := gen.AlphaString()
	urlGen := gen.RegexMatch(`https://[a-z]{3,8}\.[a-z]{2,4}`)
	skillNameGen := gen.RegexMatch(`[a-z]{2,8}`)
	skillDescGen := gen.AlphaString()

	properties.Property("JSON field names match a2a.AgentCard conventions", prop.ForAll(
		func(name, desc, url, skillName, skillDesc string) bool {
			if strings.TrimSpace(name) == "" {
				return true
			}

			card := &CallerCard{
				Name:        name,
				Description: desc,
				URL:         url,
				Skills:      []CallerSkill{{Name: skillName, Description: skillDesc}},
				Capabilities: &CallerCapabilities{
					Streaming:         true,
					PushNotifications: false,
				},
			}

			data, err := json.Marshal(card)
			if err != nil {
				return false
			}

			// Parse into a generic map to inspect field names.
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				return false
			}

			// Required fields.
			if _, ok := fields["name"]; !ok {
				return false
			}
			if _, ok := fields["description"]; !ok {
				return false
			}

			// Optional fields — should be present when set.
			if url != "" {
				if _, ok := fields["url"]; !ok {
					return false
				}
			}
			if _, ok := fields["skills"]; !ok {
				return false
			}
			if _, ok := fields["capabilities"]; !ok {
				return false
			}

			// Verify skills field structure.
			var skills []map[string]json.RawMessage
			if err := json.Unmarshal(fields["skills"], &skills); err != nil {
				return false
			}
			if len(skills) != 1 {
				return false
			}
			if _, ok := skills[0]["name"]; !ok {
				return false
			}

			// Verify capabilities field structure.
			var caps map[string]json.RawMessage
			if err := json.Unmarshal(fields["capabilities"], &caps); err != nil {
				return false
			}
			if _, ok := caps["streaming"]; !ok {
				return false
			}

			return true
		},
		nameGen,
		descGen,
		urlGen,
		skillNameGen,
		skillDescGen,
	))

	properties.TestingRun(t)
}

// ============================================================================
// Unit Tests for caller card tools (Task 6.9)
// ============================================================================

func TestCallerCard_CreateSuccess_ReturnsConfirmation(t *testing.T) {
	srv := NewServer()

	input := CreateCallerCardInput{
		Name:        "My Agent",
		Description: "Does stuff",
	}

	result, _, err := srv.handleCreateCallerCard(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if !strings.Contains(textContent.Text, "My Agent") {
		t.Errorf("expected confirmation to mention agent name, got %q", textContent.Text)
	}
}

func TestCallerCard_ViewNoCard_ReturnsInformational(t *testing.T) {
	srv := NewServer()

	result, _, err := srv.handleViewCallerCard(context.Background(), nil, ViewCallerCardInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected non-error result")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text != "no caller agent card is currently set" {
		t.Errorf("unexpected message: %q", textContent.Text)
	}
}

func TestCallerCard_RemoveNoCard_ReturnsInformational(t *testing.T) {
	srv := NewServer()

	result, _, err := srv.handleRemoveCallerCard(context.Background(), nil, RemoveCallerCardInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected non-error result")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text != "no caller agent card was set" {
		t.Errorf("unexpected message: %q", textContent.Text)
	}
}

func TestCallerCard_RemoveSuccess_ReturnsConfirmation(t *testing.T) {
	srv := NewServer()

	// Create a card first.
	input := CreateCallerCardInput{Name: "Agent", Description: "desc"}
	_, _, _ = srv.handleCreateCallerCard(context.Background(), nil, input)

	// Remove it.
	result, _, err := srv.handleRemoveCallerCard(context.Background(), nil, RemoveCallerCardInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected non-error result")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if textContent.Text != "Caller agent card removed" {
		t.Errorf("unexpected message: %q", textContent.Text)
	}
}

func TestCallerCard_DefaultMetadataKey(t *testing.T) {
	srv := NewServer()

	// Create a card without specifying metadata_key.
	input := CreateCallerCardInput{Name: "Agent", Description: "desc"}
	_, _, _ = srv.handleCreateCallerCard(context.Background(), nil, input)

	// Inject into nil metadata.
	result := srv.injectCallerCard(nil)

	// Card should be under the default key.
	if _, ok := result[defaultCallerCardKey]; !ok {
		t.Errorf("expected card under default key %q", defaultCallerCardKey)
	}
	if defaultCallerCardKey != "caller_agent_card" {
		t.Errorf("expected default key to be %q, got %q", "caller_agent_card", defaultCallerCardKey)
	}
}

func TestCallerCard_CustomMetadataKey(t *testing.T) {
	srv := NewServer()

	// Create a card with a custom metadata key.
	input := CreateCallerCardInput{
		Name:        "Agent",
		Description: "desc",
		MetadataKey: "my_custom_key",
	}
	_, _, _ = srv.handleCreateCallerCard(context.Background(), nil, input)

	// Inject into nil metadata.
	result := srv.injectCallerCard(nil)

	// Card should be under the custom key, NOT the default.
	if _, ok := result["my_custom_key"]; !ok {
		t.Error("expected card under custom key 'my_custom_key'")
	}
	if _, ok := result[defaultCallerCardKey]; ok {
		t.Error("expected card NOT under default key when custom key is set")
	}
}

// ============================================================================
// Integration Tests for metadata injection (Task 6.10)
// ============================================================================

func TestCallerCard_SendMessage_InjectsCard(t *testing.T) {
	// Set up a mock agent that captures received metadata.
	var receivedMetadata map[string]any
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		var params a2a.SendMessageRequest
		_ = json.Unmarshal(req.Params, &params)
		receivedMetadata = params.Metadata

		task := &a2a.Task{
			ID:     "task-1",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("ok")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agent.Close()

	srv := newTestServerWithAgent("inject-agent", agent.URL)

	// Register a caller card.
	cardInput := CreateCallerCardInput{
		Name:        "My IDE Agent",
		Description: "Helps with code",
	}
	_, _, _ = srv.handleCreateCallerCard(context.Background(), nil, cardInput)

	// Send a message.
	sendInput := SendMessageInput{
		Agent:   "inject-agent",
		Message: "hello",
	}
	result, _, err := srv.handleSendMessage(context.Background(), nil, sendInput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Verify the caller card was injected into metadata.
	if receivedMetadata == nil {
		t.Fatal("expected metadata to be set on outbound request")
	}
	cardData, ok := receivedMetadata[defaultCallerCardKey]
	if !ok {
		t.Fatalf("expected metadata to contain key %q", defaultCallerCardKey)
	}

	// The card is serialized as a map in JSON.
	cardMap, ok := cardData.(map[string]any)
	if !ok {
		t.Fatalf("expected card to be a map, got %T", cardData)
	}
	if cardMap["name"] != "My IDE Agent" {
		t.Errorf("expected card name %q, got %v", "My IDE Agent", cardMap["name"])
	}
	if cardMap["description"] != "Helps with code" {
		t.Errorf("expected card description %q, got %v", "Helps with code", cardMap["description"])
	}
}

func TestCallerCard_BroadcastMessage_InjectsCard(t *testing.T) {
	// Set up agents that capture received metadata.
	var receivedMetadataA, receivedMetadataB map[string]any

	agentA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		var params a2a.SendMessageRequest
		_ = json.Unmarshal(req.Params, &params)
		receivedMetadataA = params.Metadata

		task := &a2a.Task{
			ID:     "task-a",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("ok from A")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agentA.Close()

	agentB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		var params a2a.SendMessageRequest
		_ = json.Unmarshal(req.Params, &params)
		receivedMetadataB = params.Metadata

		task := &a2a.Task{
			ID:     "task-b",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("ok from B")}},
			},
		}
		writeJSONRPCResult(w, req.ID, jsonrpcTaskResult(task))
	}))
	defer agentB.Close()

	srv := NewServer()
	srv.registry.Connect("bcast-a", agentA.URL, nil)
	srv.registry.SetCard("bcast-a", &a2a.AgentCard{
		Name: "bcast-a",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(agentA.URL, a2a.TransportProtocolJSONRPC),
		},
	})
	srv.registry.Connect("bcast-b", agentB.URL, nil)
	srv.registry.SetCard("bcast-b", &a2a.AgentCard{
		Name: "bcast-b",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(agentB.URL, a2a.TransportProtocolJSONRPC),
		},
	})

	// Register a caller card.
	cardInput := CreateCallerCardInput{
		Name:        "Broadcaster",
		Description: "Sends broadcasts",
	}
	_, _, _ = srv.handleCreateCallerCard(context.Background(), nil, cardInput)

	// Broadcast.
	bcastInput := BroadcastMessageInput{
		Aliases: []string{"bcast-a", "bcast-b"},
		Message: "hello broadcast",
	}
	result, _, err := srv.handleBroadcastMessage(context.Background(), nil, bcastInput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Verify both agents received the caller card.
	for i, meta := range []map[string]any{receivedMetadataA, receivedMetadataB} {
		label := fmt.Sprintf("agent %d", i)
		if meta == nil {
			t.Fatalf("%s: expected metadata to be set", label)
		}
		cardData, ok := meta[defaultCallerCardKey]
		if !ok {
			t.Fatalf("%s: expected metadata key %q", label, defaultCallerCardKey)
		}
		cardMap, ok := cardData.(map[string]any)
		if !ok {
			t.Fatalf("%s: expected card to be a map, got %T", label, cardData)
		}
		if cardMap["name"] != "Broadcaster" {
			t.Errorf("%s: expected card name %q, got %v", label, "Broadcaster", cardMap["name"])
		}
	}
}

func TestCallerCard_StreamingPath_InjectsCard(t *testing.T) {
	// Set up a streaming agent that captures metadata.
	var receivedMetadata map[string]any
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := readJSONRPCRequest(r)
		var params a2a.SendMessageRequest
		_ = json.Unmarshal(req.Params, &params)
		receivedMetadata = params.Metadata

		// Return a completed task via SSE.
		task := &a2a.Task{
			ID:     "stream-task-card",
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []*a2a.Artifact{
				{Parts: a2a.ContentParts{a2a.NewTextPart("streamed ok")}},
			},
		}
		streamResp := map[string]any{"task": task}
		resultJSON, _ := json.Marshal(streamResp)
		rpcResp := map[string]any{
			"jsonrpc": "2.0",
			"id":      "1",
			"result":  json.RawMessage(resultJSON),
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		data, _ := json.Marshal(rpcResp)
		fmt.Fprintf(w, "data: %s\n\n", data)
	}))
	defer agent.Close()

	srv := NewServer()
	srv.registry.Connect("stream-card-agent", agent.URL, nil)
	srv.registry.SetCard("stream-card-agent", &a2a.AgentCard{
		Name:         "stream-card-agent",
		Capabilities: a2a.AgentCapabilities{Streaming: true},
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(agent.URL, a2a.TransportProtocolJSONRPC),
		},
	})

	// Register a caller card.
	cardInput := CreateCallerCardInput{
		Name:        "Streaming Caller",
		Description: "Uses streaming",
	}
	_, _, _ = srv.handleCreateCallerCard(context.Background(), nil, cardInput)

	// Send a message (should use streaming path).
	sendInput := SendMessageInput{
		Agent:   "stream-card-agent",
		Message: "hello streaming",
	}
	result, _, err := srv.handleSendMessage(context.Background(), nil, sendInput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Verify the caller card was injected into metadata on the streaming path.
	if receivedMetadata == nil {
		t.Fatal("expected metadata to be set on streaming outbound request")
	}
	cardData, ok := receivedMetadata[defaultCallerCardKey]
	if !ok {
		t.Fatalf("expected metadata to contain key %q on streaming path", defaultCallerCardKey)
	}
	cardMap, ok := cardData.(map[string]any)
	if !ok {
		t.Fatalf("expected card to be a map, got %T", cardData)
	}
	if cardMap["name"] != "Streaming Caller" {
		t.Errorf("expected card name %q, got %v", "Streaming Caller", cardMap["name"])
	}
}
