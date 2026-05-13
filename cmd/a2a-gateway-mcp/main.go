// Package main provides the CLI entry point for the A2A Gateway MCP server.
// It reads configuration from environment variables and starts the server
// on the stdio transport.
package main

import (
	"context"
	"log"
	"os"

	"github.com/nisimpson/a2a-gateway-mcp/gateway"
)

func main() {
	opts := []gateway.Option{}

	if name := os.Getenv("A2A_GATEWAY_NAME"); name != "" {
		opts = append(opts, gateway.WithName(name))
	}
	if version := os.Getenv("A2A_GATEWAY_VERSION"); version != "" {
		opts = append(opts, gateway.WithVersion(version))
	}

	srv := gateway.NewServer(opts...)
	if err := srv.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
