package tool

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCreateCallerCard_EmptyName(t *testing.T) {
	store := &mockCallerCardStore{}
	tool := &CreateCallerCardTool{Store: store}

	result, _, err := tool.Handle(context.Background(), nil, &CreateCallerCardInput{
		Name:        "   ",
		Description: "a description",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error result for whitespace-only name")
	}
	assertTextContains(t, result, "name must not be empty")
}

func TestCreateCallerCard_Success(t *testing.T) {
	store := &mockCallerCardStore{}
	tool := &CreateCallerCardTool{Store: store}

	result, _, err := tool.Handle(context.Background(), nil, &CreateCallerCardInput{
		Name:        "My Agent",
		Description: "Does cool things",
		URL:         "http://localhost:8080",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}
	if store.card == nil {
		t.Fatal("expected card to be stored")
	}
	if store.card.Name != "My Agent" {
		t.Errorf("expected name 'My Agent', got %q", store.card.Name)
	}
	if store.card.Description != "Does cool things" {
		t.Errorf("expected description 'Does cool things', got %q", store.card.Description)
	}
	assertTextContains(t, result, "Caller agent card registered")
}

func TestViewCallerCard_NoCard(t *testing.T) {
	store := &mockCallerCardStore{}
	tool := &ViewCallerCardTool{Store: store}

	result, _, err := tool.Handle(context.Background(), nil, &ViewCallerCardInput{})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatal("unexpected error result")
	}
	assertTextContains(t, result, "no caller agent card is currently set")
}

func TestViewCallerCard_WithCard(t *testing.T) {
	store := &mockCallerCardStore{
		card: &CallerCard{
			Name:        "My Agent",
			Description: "Does things",
			URL:         "http://localhost:9000",
		},
	}
	tool := &ViewCallerCardTool{Store: store}

	result, _, err := tool.Handle(context.Background(), nil, &ViewCallerCardInput{})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatal("unexpected error result")
	}

	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}

	var card CallerCard
	if err := json.Unmarshal([]byte(tc.Text), &card); err != nil {
		t.Fatalf("failed to unmarshal card JSON: %v", err)
	}
	if card.Name != "My Agent" {
		t.Errorf("expected name 'My Agent', got %q", card.Name)
	}
	if card.URL != "http://localhost:9000" {
		t.Errorf("expected url 'http://localhost:9000', got %q", card.URL)
	}
}

func TestRemoveCallerCard_NoCard(t *testing.T) {
	store := &mockCallerCardStore{}
	tool := &RemoveCallerCardTool{Store: store}

	result, _, err := tool.Handle(context.Background(), nil, &RemoveCallerCardInput{})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatal("unexpected error result")
	}
	assertTextContains(t, result, "no caller agent card was set")
}

func TestRemoveCallerCard_Success(t *testing.T) {
	store := &mockCallerCardStore{
		card: &CallerCard{Name: "My Agent", Description: "test"},
	}
	tool := &RemoveCallerCardTool{Store: store}

	result, _, err := tool.Handle(context.Background(), nil, &RemoveCallerCardInput{})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatal("unexpected error result")
	}
	if store.card != nil {
		t.Error("expected card to be nil after removal")
	}
	assertTextContains(t, result, "Caller agent card removed")
}
