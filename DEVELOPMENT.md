# Development

## Prerequisites

- Go 1.25+

## Build

```bash
make build          # Build all binaries to bin/
make gateway        # Build just the gateway binary
make mockserver     # Build the mock A2A server for testing
```

## Test

```bash
make test           # Run all tests with coverage
make test-cover     # Generate HTML coverage report in browser
```

## Quality Control

```bash
make tidy           # Format code and tidy go.mod
make audit          # Run vet, lint, vuln scan, and race-detected tests
make lint           # Full lint pipeline (tidy + audit + no uncommitted changes)
```

## Mock Server

A mock A2A server is included for local testing:

```bash
make mockserver
./bin/mockserver -port 9090 -name "test-agent"
```

It implements the minimum A2A surface: agent card discovery, message echo, and task retrieval.

## Project Structure

```
├── cmd/
│   ├── a2a-gateway-mcp/    # Standalone MCP server binary
│   └── mockserver/          # Mock A2A server for testing
├── gateway/                 # MCP gateway library
│   ├── server.go            # Server constructor and options
│   ├── registry.go          # Thread-safe agent registry
│   ├── context_store.go     # Conversation context management
│   ├── resolve.go           # Agent identifier resolution
│   ├── tools.go             # Tool registration
│   ├── tool_connect.go      # connect_agent handler
│   ├── tool_disconnect.go   # disconnect_agent handler
│   ├── tool_list.go         # list_agents handler
│   ├── tool_card.go         # get_agent_card handler
│   ├── tool_send.go         # send_message handler
│   ├── tool_broadcast.go    # broadcast_message handler
│   ├── tool_discover.go     # discover_agents handler
│   ├── response.go          # A2A → MCP response formatting
│   ├── validate.go          # Input validation
│   └── http.go              # HTTP client composition
├── directory/               # Agent directory service
│   ├── directory.go         # Directory struct and constructor
│   ├── registry.go          # Registry interface and MemoryRegistry
│   ├── resolver.go          # FilterResolver and DefaultResolver
│   ├── handler.go           # HTTP handler (ServeHTTP)
│   ├── server.go            # Standalone server (ListenAndServe)
│   └── options.go           # Functional options
└── internal/                # Unexported shared helpers
```

## Testing Philosophy

The project uses property-based testing (via [gopter](https://github.com/leanovate/gopter)) alongside traditional unit and integration tests. Formal correctness properties are defined for:

- Agent identifier resolution consistency
- Context lifecycle correctness
- Registry concurrent safety
- Response text extraction fidelity
- JSON serialization round-trips
- Input validation completeness

Each property is validated with a minimum of 100 random test iterations.

## Makefile Reference

Run `make help` for a full list of targets:

```
  help         print this help message
  build        build all binaries
  clean        remove build artifacts
  tidy         format code and tidy modfile
  audit        run quality control checks
  lint         run tidy, audit, and verify no uncommitted changes
  test         run all tests
  test-cover   run tests with coverage report in browser
  push         push changes to the remote Git repository
```
