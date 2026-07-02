package directory

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// encodeCursor encodes an offset into an opaque base64 cursor token.
func encodeCursor(offset int) string {
	return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("offset:%d", offset)))
}

// decodeCursor decodes an opaque base64 cursor token into an offset.
// Returns 0 if the cursor is empty or invalid.
func decodeCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	data, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("invalid cursor")
	}
	var offset int
	_, err = fmt.Sscanf(string(data), "offset:%d", &offset)
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid cursor")
	}
	return offset, nil
}

// ServeHTTP handles HTTP requests for the directory endpoint.
// It supports GET requests with optional "filter", "limit", and "cursor" query parameters.
// Responses are always returned as a QueryResult JSON object.
func (d *Directory) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	params := r.URL.Query()

	// Check for help request BEFORE processing filter/limit/cursor.
	// Help resolution priority: Registry FilterHelper → FilterResolver FilterHelper → DefaultFilterHelp().
	if params.Get("help") == "true" {
		var helpResp FilterHelpResponse
		if helper, ok := d.registry.(FilterHelper); ok {
			helpResp = helper.FilterHelp()
		} else if helper, ok := d.resolver.(FilterHelper); ok {
			helpResp = helper.FilterHelp()
		} else {
			helpResp = DefaultFilterHelp()
		}
		result := QueryResult{
			Cards:    []a2a.AgentCard{},
			HelpResp: &helpResp,
		}
		writeJSON(w, http.StatusOK, result)
		return
	}

	query := params.Get("filter")
	limitStr := params.Get("limit")
	cursorStr := params.Get("cursor")

	ctx := r.Context()

	// Priority 1: Querier — full delegation with opaque cursor.
	if querier, ok := d.registry.(Querier); ok {
		// Parse limit for the Querier path: 0 means "no limit", pass through as-is.
		var limit int
		if limitStr != "" {
			parsed, err := strconv.Atoi(limitStr)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be a positive integer"})
				return
			}
			limit = parsed
		}

		// Do NOT hold any mutex across the Query call.
		cards, nextCursor, err := querier.Query(ctx, query, limit, cursorStr)
		if err != nil {
			if errors.Is(err, ErrInvalidCursor) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cursor"})
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			}
			return
		}

		// Normalize nil cards to empty slice for JSON serialization.
		if cards == nil {
			cards = []a2a.AgentCard{}
		}

		result := QueryResult{
			Cards:     cards,
			NextToken: nextCursor,
		}
		writeJSON(w, http.StatusOK, result)
		return
	}

	// Priority 2 & 3: Filterer or List+FilterResolver with offset-based cursor.

	// Decode cursor to get the offset.
	offset, err := decodeCursor(cursorStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cursor"})
		return
	}

	var limit int
	var hasLimit bool
	if limitStr != "" {
		hasLimit = true
		parsed, err := strconv.Atoi(limitStr)
		if err != nil || parsed <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be a positive integer"})
			return
		}
		limit = parsed
	}

	var cards []a2a.AgentCard

	if query != "" {
		if filterer, ok := d.registry.(Filterer); ok {
			cards, err = filterer.Filter(ctx, query)
		} else {
			cards, err = d.registry.List(ctx)
			if err == nil {
				cards = d.resolver.Resolve(ctx, query, cards)
			}
		}
	} else {
		cards, err = d.registry.List(ctx)
	}

	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	// Apply cursor offset.
	if offset > len(cards) {
		offset = len(cards)
	}
	cards = cards[offset:]

	// Apply limit and compute next cursor.
	var nextToken string
	if hasLimit && len(cards) > limit {
		cards = cards[:limit]
		nextToken = encodeCursor(offset + limit)
	}

	if cards == nil {
		cards = []a2a.AgentCard{}
	}

	result := QueryResult{
		Cards:     cards,
		NextToken: nextToken,
	}
	writeJSON(w, http.StatusOK, result)
}

// writeJSON serializes v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
