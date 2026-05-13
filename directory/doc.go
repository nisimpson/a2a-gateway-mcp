// Package directory implements a server-side A2A agent directory service that
// stores agent cards and exposes them via an HTTP GET endpoint.
//
// It serves as the counterpart to the gateway's discover_agents tool, allowing
// clients to discover registered agents through optional query filtering and
// result limiting.
//
// # Usage
//
// Create a directory with default settings (in-memory registry, default resolver):
//
//	dir := directory.New()
//
// Register agent cards programmatically:
//
//	dir.Register(ctx, a2a.AgentCard{Name: "my-agent", ...})
//
// # Embedding as an http.Handler
//
// The Directory implements http.Handler and can be mounted on any ServeMux:
//
//	mux := http.NewServeMux()
//	mux.Handle("/agents", dir)
//
// Clients issue GET requests with optional filter and limit parameters:
//
//	GET /agents?filter=weather&limit=10
//
// # Standalone Server
//
// Run the directory as a standalone HTTP server:
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//	dir.ListenAndServe(ctx, ":8080")
//
// # Custom Backends
//
// The Registry interface abstracts storage, allowing custom implementations
// (e.g., persistent backends like Redis or DynamoDB):
//
//	dir := directory.New(
//	    directory.WithRegistry(myCustomRegistry),
//	    directory.WithQueryResolver(myCustomResolver),
//	)
//
// Registries that support native server-side filtering can implement the
// optional Filterer interface to push filtering down to the storage layer.
package directory
