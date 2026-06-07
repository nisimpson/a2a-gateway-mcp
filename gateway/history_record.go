package gateway

import (
	"context"
	"log"
	"time"
)

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

// recordHistory appends an interaction to the history backend.
// It truncates sent/response text and is safe to call from any goroutine.
// Non-fatal: logs a warning if the backend returns an error but does not
// propagate the error to the caller.
// No-op when historyEnabled is false.
func (s *Server) recordHistory(ctx context.Context, alias, sent, response, contextID, taskID string, isError bool) {
	if !s.historyEnabled {
		return
	}

	entry := HistoryEntry{
		Timestamp: time.Now().UTC(),
		SentMsg:   truncateText(sent, s.maxEntryLength),
		Response:  truncateText(response, s.maxEntryLength),
		ContextID: contextID,
		TaskID:    taskID,
		IsError:   isError,
	}

	if err := s.historyBackend.Append(ctx, alias, entry); err != nil {
		log.Printf("warning: failed to record history for alias %q: %v", alias, err)
	}
}
