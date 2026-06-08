package gateway

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// PingTarget is a read-only snapshot of the agent information needed for
// liveness checks. It is constructed by the gateway before calling the
// PingStrategy, ensuring strategy implementations cannot mutate internal
// registry state.
type PingTarget struct {
	Alias        string            // agent alias
	URL          string            // agent base URL
	Headers      map[string]string // copied headers (safe to read, mutations have no effect)
	PingEndpoint string            // custom ping path (empty = use default)
}

// PingResult holds the outcome of a ping operation.
type PingResult struct {
	Reachable    bool          // whether the agent responded
	ResponseTime time.Duration // round-trip time (zero if unreachable)
	Err          error         // underlying error if unreachable (for classification)
}

// PingStrategy defines how liveness checks are performed.
// Implementations must be safe for concurrent use.
type PingStrategy interface {
	// Ping performs a liveness check for the given agent target.
	// The context carries a timeout (default 5s); implementations should respect it.
	Ping(ctx context.Context, target PingTarget) PingResult
}

// DefaultPingStrategy performs an HTTP GET to the agent's configured
// PingEndpoint, or falls back to .well-known/agent.json.
// It uses the server's existing HTTP client (the one configured via
// WithHTTPClient or the default) — it does NOT create its own client.
type DefaultPingStrategy struct {
	client *http.Client // reuses s.httpClient; never allocated internally
}

// NewDefaultPingStrategy wraps an existing HTTP client for ping requests.
// The caller is responsible for providing the client (typically s.httpClient).
func NewDefaultPingStrategy(client *http.Client) *DefaultPingStrategy {
	return &DefaultPingStrategy{client: client}
}

// Ping performs an HTTP GET to the target's ping endpoint using the
// shared HTTP client. Per-agent headers from PingTarget.Headers are
// applied to the request.
func (d *DefaultPingStrategy) Ping(ctx context.Context, target PingTarget) PingResult {
	// Determine the ping URL.
	pingURL := buildPingURL(target.URL, target.PingEndpoint)

	// Create an HTTP GET request with the provided context (carries timeout).
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pingURL, nil)
	if err != nil {
		return PingResult{Reachable: false, Err: err}
	}

	// Apply per-agent headers.
	for k, v := range target.Headers {
		req.Header.Set(k, v)
	}

	// Execute the request and measure response time.
	start := time.Now()
	resp, err := d.client.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		return PingResult{Reachable: false, Err: err}
	}
	defer resp.Body.Close()

	// Any HTTP response (any status code) means the agent is reachable.
	return PingResult{Reachable: true, ResponseTime: elapsed}
}

// buildPingURL constructs the full URL for the ping request.
// If pingEndpoint is non-empty, it is joined to the base URL.
// Otherwise, defaults to .well-known/agent.json relative to the base URL.
func buildPingURL(baseURL, pingEndpoint string) string {
	base := strings.TrimRight(baseURL, "/")
	if pingEndpoint != "" {
		// pingEndpoint is expected to start with "/", but handle both cases.
		if !strings.HasPrefix(pingEndpoint, "/") {
			return base + "/" + pingEndpoint
		}
		return base + pingEndpoint
	}
	return base + "/.well-known/agent.json"
}
