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
	start := run
	if len(os.Args) > 1 && os.Args[1] == "providers" {
		start = func(ctx context.Context) error { return runProviders(ctx, os.Args[2:]) }
	}
	if err := start(ctx); err != nil {
		slog.Error("Retrom stopped", "error", err)
		stop()
		os.Exit(1)
	}
	stop()
}
