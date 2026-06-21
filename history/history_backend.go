package history

import (
	"context"
	"fmt"
	"time"
)

// Entry represents a single recorded interaction with an agent.
type Entry struct {
	Timestamp   string `json:"timestamp" jsonschema:"ISO 8601 timestamp of the interaction"`
	SentMessage string `json:"sent_message" jsonschema:"message sent to the agent"`
	Response    string `json:"response" jsonschema:"response received from the agent"`
	ContextID   string `json:"context_id,omitempty" jsonschema:"context identifier if present"`
	TaskID      string `json:"task_id,omitempty" jsonschema:"task identifier if present"`
	IsError     bool   `json:"is_error,omitempty" jsonschema:"whether the interaction resulted in an error"`
}

// ParseTimestamp parses the entry's ISO 8601 timestamp string into a time.Time value.
// It returns the zero time if the Timestamp field is empty.
// It panics if the Timestamp field is non-empty but cannot be parsed as RFC 3339.
func (e Entry) ParseTimestamp() time.Time {
	if e.Timestamp == "" {
		return time.Time{}
	}
	ts, err := time.Parse(time.RFC3339, e.Timestamp)
	if err != nil {
		panic(fmt.Errorf("unable to parse timestamp"))
	}
	return ts
}

// Backend is the pluggable storage interface for interaction history.
// Implementations must be safe for concurrent use by multiple goroutines.
// All methods accept a context.Context as the first parameter, carrying
// request-scoped values (deadlines, cancellation, tracing, logging state).
type Backend interface {
	// Append adds a new entry to the history for the given alias.
	// The backend is responsible for enforcing any depth limits.
	Append(ctx context.Context, alias string, entry Entry) error

	// List returns all stored entries for the alias in chronological order
	// (oldest first). Returns an empty slice if the alias has no history.
	List(ctx context.Context, alias string) ([]Entry, error)

	// Delete removes all history entries for the given alias.
	Delete(ctx context.Context, alias string) error

	// Clear removes all history entries for the given alias.
	// Semantically identical to Delete — provided for naming clarity at call sites.
	Clear(ctx context.Context, alias string) error

	// Close signals the backend to perform any cleanup (flush buffers, close
	// file handles, release connections). Called once during server shutdown.
	Close(ctx context.Context) error
}
