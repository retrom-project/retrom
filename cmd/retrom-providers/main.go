// Command retrom-providers is the Provider chunk tooling entry. It depends only on
// internal/providersync, so it builds on a clean checkout without generated API code.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"retrom/internal/providersync"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	if err := providersync.RunCLI(ctx, os.Args[1:]); err != nil {
		slog.Error("Provider synchronization failed", "error", err)
		stop()
		os.Exit(1)
	}
	stop()
}
