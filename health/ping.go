package health

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// PingTarget is a read-only snapshot of the agent information needed for
// liveness checks.
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
	Ping(ctx context.Context, target PingTarget) PingResult
}

// DefaultPingStrategy performs an HTTP GET to the agent's configured
// PingEndpoint, or falls back to .well-known/agent-card.json.
type DefaultPingStrategy struct {
	client *http.Client
}

// NewDefaultPingStrategy wraps an existing HTTP client for ping requests.
func NewDefaultPingStrategy(client *http.Client) *DefaultPingStrategy {
	return &DefaultPingStrategy{client: client}
}

// Ping performs an HTTP GET to the target's ping endpoint using the
// shared HTTP client. Per-agent headers from PingTarget.Headers are
// applied to the request.
func (d *DefaultPingStrategy) Ping(ctx context.Context, target PingTarget) PingResult {
	pingURL := buildPingURL(target.URL, target.PingEndpoint)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pingURL, nil)
	if err != nil {
		return PingResult{Reachable: false, Err: err}
	}

	for k, v := range target.Headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := d.client.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		return PingResult{Reachable: false, Err: err}
	}
	defer resp.Body.Close()

	return PingResult{Reachable: true, ResponseTime: elapsed}
}

func buildPingURL(baseURL, pingEndpoint string) string {
	base := strings.TrimRight(baseURL, "/")
	if pingEndpoint != "" {
		if !strings.HasPrefix(pingEndpoint, "/") {
			return base + "/" + pingEndpoint
		}
		return base + pingEndpoint
	}
	return base + "/.well-known/agent-card.json"
}
