package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	if err := run(ctx); err != nil {
		slog.Error("Retrom stopped", "error", err)
		stop()
		os.Exit(1)
	}
	stop()
}
