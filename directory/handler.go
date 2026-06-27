package directory

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// ServeHTTP handles HTTP requests for the directory endpoint.
// It supports GET requests with optional "filter" and "limit" query parameters.
func (d *Directory) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	params := r.URL.Query()

	// Check for help request BEFORE processing filter/limit.
	if params.Get("help") == "true" {
		var helpResp FilterHelpResponse
		if helper, ok := d.resolver.(FilterHelper); ok {
			helpResp = helper.FilterHelp()
		} else {
			helpResp = DefaultFilterHelp()
		}
		writeJSON(w, http.StatusOK, helpResp)
		return
	}

	query := params.Get("filter")
	limitStr := params.Get("limit")

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

	ctx := r.Context()
	var cards []a2a.AgentCard
	var err error

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

	if hasLimit && len(cards) > limit {
		cards = cards[:limit]
	}

	if cards == nil {
		cards = []a2a.AgentCard{}
	}

	writeJSON(w, http.StatusOK, cards)
}

// writeJSON serializes v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
