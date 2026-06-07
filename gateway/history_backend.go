package gateway

import (
	"context"
	"time"
)

// HistoryEntry represents a single recorded interaction with an agent.
type HistoryEntry struct {
	Timestamp time.Time `json:"timestamp"`
	SentMsg   string    `json:"sent_message"`
	Response  string    `json:"response"`
	ContextID string    `json:"context_id,omitempty"`
	TaskID    string    `json:"task_id,omitempty"`
	IsError   bool      `json:"is_error,omitempty"`
}

// HistoryBackend is the pluggable storage interface for interaction history.
// Implementations must be safe for concurrent use by multiple goroutines.
// All methods accept a context.Context as the first parameter, carrying
// request-scoped values (deadlines, cancellation, tracing, logging state).
type HistoryBackend interface {
	// Append adds a new entry to the history for the given alias.
	// The backend is responsible for enforcing any depth limits.
	Append(ctx context.Context, alias string, entry HistoryEntry) error

	// List returns all stored entries for the alias in chronological order
	// (oldest first). Returns an empty slice if the alias has no history.
	List(ctx context.Context, alias string) ([]HistoryEntry, error)

	// Delete removes all history entries for the given alias.
	Delete(ctx context.Context, alias string) error

	// Clear removes all history entries for the given alias.
	// Semantically identical to Delete — provided for naming clarity at call sites.
	Clear(ctx context.Context, alias string) error

	// Close signals the backend to perform any cleanup (flush buffers, close
	// file handles, release connections). Called once during server shutdown.
	Close(ctx context.Context) error
}
