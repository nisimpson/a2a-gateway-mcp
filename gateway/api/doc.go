// Package api defines the public input and output types for A2A gateway
// operations. These types are shared between the gateway package's public
// Server methods and the internal tool implementations, ensuring a single
// source of truth for the operation contracts.
//
// Embedders who build custom MCP tools can use these types directly with
// the programmatic API on [gateway.Server].
package api
