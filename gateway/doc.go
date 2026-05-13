// Package gateway implements an MCP server that acts as a multi-tenant gateway
// to the A2A (Agent-to-Agent) protocol. It enables MCP clients to dynamically
// connect to, manage, and communicate with multiple remote A2A agents through
// an ephemeral, session-scoped registry.
//
// # Tools
//
// The server exposes seven MCP tools:
//
//   - connect_agent: Register a remote A2A agent with a friendly alias
//   - disconnect_agent: Remove a registered agent by alias
//   - list_agents: List all connected agents with their aliases and URLs
//   - get_agent_card: Retrieve an agent's capabilities from its card endpoint
//   - send_message: Send a text message to an agent by alias or URL
//   - broadcast_message: Send the same message to multiple agents concurrently
//   - discover_agents: Query a remote agent directory for available agents
//
// # Usage
//
// Create and run a gateway server with default settings:
//
//	srv := gateway.NewServer()
//	if err := srv.Run(ctx); err != nil {
//	    log.Fatal(err)
//	}
//
// The server communicates over stdio using JSON-RPC, making it compatible
// with any MCP client.
//
// # Configuration
//
// Use functional options to customize the server:
//
//	srv := gateway.NewServer(
//	    gateway.WithName("my-gateway"),
//	    gateway.WithVersion("1.0.0"),
//	    gateway.WithHTTPClient(customClient),
//	)
//
// A custom http.Client allows injecting authentication, retries, or
// observability into all outbound A2A requests.
//
// # Agent Management
//
// Agents are registered at runtime via the connect_agent tool. Each agent
// is identified by a short alias (lowercase alphanumeric + hyphens) and
// associated with a URL and optional static HTTP headers. The registry is
// in-memory and does not persist across server restarts.
//
// Per-agent headers are injected via a custom http.RoundTripper that composes
// with the server's base http.Client, allowing the user-provided transport
// to observe all headers on outbound requests.
//
// # Conversation Context
//
// The gateway maintains conversation state per agent alias. When an A2A agent
// returns a context ID in its response, the gateway stores it and automatically
// attaches it to subsequent messages to the same agent. Clients can also
// provide an explicit context ID to override the stored value.
//
// # Error Handling
//
// All tool errors are returned as MCP error responses (IsError: true) with
// descriptive text. The server never panics on tool errors and remains
// operational after any failure. Broadcast operations support partial failure,
// attributing success or error status to each individual agent.
package gateway
