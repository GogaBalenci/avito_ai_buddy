// Command api is the HTTP API of AI-buddy.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/GogaBalenci/avito_ai_buddy/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, os.Getenv, nil); err != nil {
		slog.Error("api stopped", "error", err.Error())
		os.Exit(1)
	}
}
