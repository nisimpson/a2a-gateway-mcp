package registry

import (
	"net/http"
	"strings"
)

// HeaderProvider is a function that returns the current set of headers
// to inject into outgoing HTTP requests. By using a provider instead of
// a static map, callers can supply headers that refresh over time (e.g.,
// rotated auth tokens read from the registry on each request).
type HeaderProvider func() map[string]string

// headerRoundTripper wraps an http.RoundTripper and injects headers
// obtained from a [HeaderProvider] before delegating to the base transport.
type headerRoundTripper struct {
	base     http.RoundTripper
	provider HeaderProvider
}

// RoundTrip clones the request, injects headers from the provider (skipping
// protocol-reserved headers), and delegates to the base RoundTripper.
func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	for k, v := range h.provider() {
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

// HTTPClientWithHeaderProvider returns an *http.Client that resolves
// headers dynamically via the given [HeaderProvider] on every request.
// If provider returns an empty map, no extra headers are injected.
func HTTPClientWithHeaderProvider(base *http.Client, provider HeaderProvider) *http.Client {
	transport := base.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	return &http.Client{
		Timeout: base.Timeout,
		Transport: &headerRoundTripper{
			base:     transport,
			provider: provider,
		},
	}
}

// HTTPClientForAgent returns an *http.Client configured with the agent's
// static headers composed on top of the given base http.Client.
// If the entry has no headers, the base client is returned as-is.
//
// For long-lived clients where headers may rotate, prefer
// [HTTPClientWithHeaderProvider] instead.
func HTTPClientForAgent(base *http.Client, entry *RegisteredAgent) *http.Client {
	if len(entry.Headers) == 0 {
		return base
	}

	headers := entry.Headers
	return HTTPClientWithHeaderProvider(base, func() map[string]string {
		return headers
	})
}
