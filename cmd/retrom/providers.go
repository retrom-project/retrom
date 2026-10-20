package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"retrom/internal/providersync"
	"retrom/internal/runtimeclient"
)

// synchronizeProviders installs the image's pinned Providers before the runtime opens.
// Without RETROM_PROVIDER_INPUTS (PFB and development), the existing base is used as is.
func synchronizeProviders(ctx context.Context, root string) ([]runtimeclient.Option, error) {
	inputs := os.Getenv("RETROM_PROVIDER_INPUTS")
	if inputs == "" {
		return nil, nil
	}
	result, err := providersync.Sync(ctx, providersync.Options{
		Inputs: inputs, Root: root, Source: os.Getenv("RETROM_RUNTIME_INPUT_BASE_URL"),
		Mode: providersync.ModeProduction, Full: os.Getenv("RETROM_PROVIDER_VERIFY") == "full",
	})
	if err != nil {
		return nil, fmt.Errorf("synchronize Providers: %w", err)
	}
	slog.Info("Providers ready", "reused", result.Reused, "downloadedChunks", result.DownloadedChunks,
		"downloadedBytes", result.DownloadedBytes, "repairedChunks", result.RepairedChunks,
		"removedChunks", result.RemovedChunks, "versions", result.ProviderVersions)
	return []runtimeclient.Option{runtimeclient.VerifiedInstall()}, nil
}
