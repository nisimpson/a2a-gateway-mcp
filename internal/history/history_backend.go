package history

import (
	"context"
	"time"
)

// Entry represents a single recorded interaction with an agent.
type Entry struct {
	Timestamp time.Time `json:"timestamp"`            // When the interaction occurred
	SentMsg   string    `json:"sent_message"`         // Message sent to the agent
	Response  string    `json:"response"`             // Agent's reply
	ContextID string    `json:"context_id,omitempty"` // Conversation thread identifier
	TaskID    string    `json:"task_id,omitempty"`    // Associated task identifier
	IsError   bool      `json:"is_error,omitempty"`   // Whether the response was an error
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
