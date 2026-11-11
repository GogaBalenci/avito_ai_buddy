// Command mcp-mocks serves the MCP mocks of corporate services.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/server"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := server.Run(ctx, os.Getenv, nil); err != nil {
		slog.Error("mcp-mocks stopped", "error", err.Error())
		os.Exit(1)
	}
}
