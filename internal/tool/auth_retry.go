package tool

import (
	"context"
	"errors"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/nisimpson/a2a-gateway-mcp/registry"
)

// isAuthError reports whether the error indicates an authentication or
// authorization failure from the remote agent (HTTP 401/403). The a2a-go
// SDK surfaces these as [a2a.ErrUnauthenticated] and [a2a.ErrUnauthorized].
func isAuthError(err error) bool {
	return errors.Is(err, a2a.ErrUnauthenticated) || errors.Is(err, a2a.ErrUnauthorized)
}

// resolveClientWithAuthRetry resolves an A2A client for the given agent,
// executes fn, and if fn returns an auth error (401/403), evicts the cached
// client and retries once with a freshly resolved client.
//
// This provides a safety net for cases where the cached client's transport
// has become stale despite the per-request header provider (e.g., corrupted
// TLS state, middleware-level caching, or intermediary credential stores).
func resolveClientWithAuthRetry(
	ctx context.Context,
	resolver A2AClientResolver,
	resolved *registry.ResolveResult,
	fn func(client *a2aclient.Client) error,
) error {
	client, err := resolver.Resolve(ctx, resolved)
	if err != nil {
		return handleA2AError(err)
	}

	err = fn(client)
	if err == nil || !isAuthError(err) {
		return err
	}

	// Auth failure: evict the cached client and retry once.
	resolver.Evict(resolved.URL)

	client, err = resolver.Resolve(ctx, resolved)
	if err != nil {
		return handleA2AError(err)
	}

	return fn(client)
}
