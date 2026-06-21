package registry

import (
	"net/http"
	"strings"
)

// headerRoundTripper wraps an http.RoundTripper and injects static headers
// before delegating to the base transport.
type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

// RoundTrip clones the request, injects static headers (skipping protocol-reserved
// headers), and delegates to the base RoundTripper.
func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	for k, v := range h.headers {
		if isProtocolHeader(k) {
			continue
		}
		clone.Header.Set(k, v)
	}
	return h.base.RoundTrip(clone)
}

// isProtocolHeader returns true for headers that must not be overridden
// (Content-Type, Accept).
func isProtocolHeader(name string) bool {
	return strings.EqualFold(name, "Content-Type") || strings.EqualFold(name, "Accept")
}

// HTTPClientForAgent returns an *http.Client configured with the agent's
// static headers composed on top of the given base http.Client.
// If the entry has no headers, the base client is returned as-is.
func HTTPClientForAgent(base *http.Client, entry *RegisteredAgent) *http.Client {
	if len(entry.Headers) == 0 {
		return base
	}

	transport := base.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	return &http.Client{
		Timeout: base.Timeout,
		Transport: &headerRoundTripper{
			base:    transport,
			headers: entry.Headers,
		},
	}
}
