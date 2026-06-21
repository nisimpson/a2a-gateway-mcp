package history

import (
	"context"
	"log"
	"time"
)

type Recorder struct {
	Backend
	Enabled        bool
	MaxEntryLength int
}

type RecordInput struct {
	Alias     string // agent alias this interaction belongs to
	Sent      string // message sent to the agent
	Response  string // response received from the agent
	ContextID string // optional context identifier for grouping
	TaskID    string // optional task identifier for grouping
	IsError   bool   // whether the interaction resulted in an error
}

// Record appends an interaction to the history backend.
// It truncates sent/response text and is safe to call from any goroutine.
// Non-fatal: logs a warning if the backend returns an error but does not
// propagate the error to the caller.
// No-op when historyEnabled is false.
func (r Recorder) Record(ctx context.Context, input RecordInput) {
	if !r.Enabled {
		return
	}

	entry := Entry{
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		SentMessage: truncateText(input.Sent, r.MaxEntryLength),
		Response:    truncateText(input.Response, r.MaxEntryLength),
		ContextID:   input.ContextID,
		TaskID:      input.TaskID,
		IsError:     input.IsError,
	}

	if err := r.Append(ctx, input.Alias, entry); err != nil {
		log.Printf("warning: failed to record history for alias %q: %v", input.Alias, err)
	}
}

// truncateText truncates text to maxLen runes, appending "…" if truncated.
// If maxLen is <= 0, the original text is returned unchanged.
func truncateText(text string, maxLen int) string {
	if maxLen <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen]) + "…"
}
