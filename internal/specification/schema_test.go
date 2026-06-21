package specification

import "testing"

func TestMessageSchema(t *testing.T) {
	schema, err := MessageSchema()
	if err != nil {
		t.Errorf("expected no error, got %s", err)
		return
	}
	if schema.Title != "A2AMessage" {
		t.Errorf("expected A2AMessage title, got %s", schema.Title)
		return
	}
}

func TestTaskSchema(t *testing.T) {
	schema, err := TaskSchema()
	if err != nil {
		t.Errorf("expected no error, got %s", err)
		return
	}
	if schema.Title != "A2ATask" {
		t.Errorf("expected A2ATask title, got %s", schema.Title)
		return
	}
}

func TestSendMessageResponseSchema(t *testing.T) {
	schema, err := SendMessageResponseSchema()
	if err != nil {
		t.Errorf("expected no error, got %s", err)
		return
	}
	if schema.Title != "SendMessageResponse" {
		t.Errorf("expected SendMessageResponse title, got %s", schema.Title)
		return
	}
}
