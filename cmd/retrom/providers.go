package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"retrom/internal/providersync"
	"retrom/internal/runtimeclient"
)

var errProvidersUsage = errors.New("usage: retrom providers sync|verify|gc [options]")

type rootList []string

func (r *rootList) String() string { return strings.Join(*r, ",") }

func (r *rootList) Set(value string) error {
	*r = append(*r, value)
	return nil
}

// runProviders exposes the single chunk synchronization implementation to tooling.
func runProviders(ctx context.Context, arguments []string) error {
	if len(arguments) == 0 {
		return errProvidersUsage
	}
	switch arguments[0] {
	case "sync", "verify":
		return providersSync(ctx, arguments[0] == "verify", arguments[1:])
	case "gc":
		return providersGC(arguments[1:])
	default:
		return errProvidersUsage
	}
}

func providersSync(ctx context.Context, full bool, arguments []string) error {
	flags := flag.NewFlagSet("providers", flag.ContinueOnError)
	options := providersync.Options{Full: full}
	var mode string
	flags.StringVar(&options.Inputs, "inputs", "", "pinned runtime-inputs.json")
	flags.StringVar(&options.Root, "root", "", "Provider root receiving active.json and installed/")
	flags.StringVar(&options.Cache, "cache", "", "chunk cache directory (default <root>/chunks)")
	flags.StringVar(&options.Source, "source", "", "chunk source directory or HTTP(S) base (default: GitHub Release)")
	flags.StringVar(&mode, "mode", string(providersync.ModeTooling), "production or tooling")
	seeds := rootList{}
	flags.Var(&seeds, "seed", "directory of verified chunks to link before downloading (repeatable)")
	if err := flags.Parse(arguments); err != nil {
		return fmt.Errorf("parse providers options: %w", err)
	}
	if options.Inputs == "" || options.Root == "" || flags.NArg() != 0 {
		return errProvidersUsage
	}
	options.Mode, options.Seeds = providersync.Mode(mode), seeds
	result, err := providersync.Sync(ctx, options)
	if err != nil {
		return fmt.Errorf("synchronize Providers: %w", err)
	}
	return writeResult(result)
}

func providersGC(arguments []string) error {
	flags := flag.NewFlagSet("providers gc", flag.ContinueOnError)
	var cache string
	var roots rootList
	flags.StringVar(&cache, "cache", "", "shared chunk cache")
	flags.Var(&roots, "root", "Provider root whose marker keeps chunks (repeatable)")
	if err := flags.Parse(arguments); err != nil {
		return fmt.Errorf("parse providers gc options: %w", err)
	}
	if cache == "" || flags.NArg() != 0 {
		return errProvidersUsage
	}
	removed, err := providersync.CollectGarbage(cache, roots)
	if err != nil {
		return fmt.Errorf("collect chunk garbage: %w", err)
	}
	return writeResult(map[string]int{"removedChunks": removed})
}

func writeResult(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("write result: %w", err)
	}
	return nil
}

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
