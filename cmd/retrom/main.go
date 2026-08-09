package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/config"
	"retrom/internal/dependencies"
	"retrom/internal/httpapi"
	"retrom/internal/importing"
	"retrom/internal/maintenance"
	"retrom/internal/processlock"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/store"
)

var (
	errBackupArgument  = errors.New("BACKUP_ARGUMENT_INVALID")
	errRestoreArgument = errors.New("RESTORE_ARGUMENT_INVALID")
	errCommand         = errors.New("COMMAND_INVALID")
)

func main() {
	if err := execute(os.Args[1:]); err != nil {
		slog.Error("retrom stopped", "error", err)
		os.Exit(1)
	}
}

//nolint:gocyclo // Each CLI command owns a small, explicit argument contract.
func execute(arguments []string) error {
	worker, err := importing.RunArchiveWorker(arguments)
	if worker {
		if err != nil {
			return fmt.Errorf("retrom/archive worker: %w", err)
		}
		return nil
	}
	if len(arguments) == 0 {
		return run(config.ModeRelease)
	}
	if arguments[0] == "--mode" || strings.HasPrefix(arguments[0], "--mode=") {
		mode, parseErr := parseServeMode(arguments)
		if parseErr != nil {
			return parseErr
		}
		return run(mode)
	}
	switch arguments[0] {
	case "backup":
		flags := flag.NewFlagSet("retrom backup", flag.ContinueOnError)
		output := flags.String("output", "", "absolute path for a new backup bundle")
		if err := flags.Parse(arguments[1:]); err != nil || *output == "" || flags.NArg() != 0 {
			return errBackupArgument
		}
		configuration, err := config.LoadBackupMaintenance()
		if err != nil {
			return fmt.Errorf("retrom/main: %w", err)
		}
		manifest, err := maintenance.Backup(context.Background(), configuration, *output, time.Now)
		if err != nil {
			return fmt.Errorf("retrom/main: %w", err)
		}
		return writeCommandResult(
			map[string]any{"status": "backup_complete", "output": *output, "fileCount": manifest.Counts.FileCount},
		)
	case "restore":
		flags := flag.NewFlagSet("retrom restore", flag.ContinueOnError)
		input := flags.String("input", "", "absolute path to a backup bundle")
		output := flags.String("output-data-dir", "", "absolute path for a new data root")
		if err := flags.Parse(arguments[1:]); err != nil || *input == "" || *output == "" || flags.NArg() != 0 {
			return errRestoreArgument
		}
		configuration, err := config.LoadRestoreMaintenance()
		if err != nil {
			return fmt.Errorf("retrom/main: %w", err)
		}
		manifest, err := maintenance.Restore(context.Background(), configuration, *input, *output)
		if err != nil {
			return fmt.Errorf("retrom/main: %w", err)
		}
		return writeCommandResult(
			map[string]any{
				"status":                          "restore_complete",
				"outputDataDir":                   *output,
				"requiredDependencyVersions":      manifest.DependencyVersions,
				"requiredActiveEmulatorjsVersion": manifest.ActiveEmulatorjsVersion,
			},
		)
	default:
		return errCommand
	}
}

func writeCommandResult(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("encode command result: %w", err)
	}
	return nil
}

//nolint:funlen,gocyclo // Process bootstrap branches are independent fail-fast checks kept in startup order.
func run(mode config.Mode) error {
	configuration, err := config.Load(mode)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	level := slog.LevelInfo
	if err := level.UnmarshalText([]byte(configuration.LogLevel)); err != nil {
		return fmt.Errorf("parse log level: %w", err)
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))

	lock, err := processlock.Acquire(configuration.DataDir)
	if err != nil {
		return fmt.Errorf("retrom/main: %w", err)
	}
	defer func() {
		if closeErr := lock.Close(); closeErr != nil {
			slog.Error("release data root lock", "error", closeErr)
		}
	}()

	startupContext, cancelStartup := context.WithTimeout(context.Background(), configuration.StartupCheckTimeout)
	defer cancelStartup()
	dependencySet, err := dependencies.Load(
		configuration.DependencyRoot,
		configuration.DependencyVersions,
		configuration.ActiveEJSVersion,
	)
	if err != nil {
		return fmt.Errorf("verify dependencies: %w", err)
	}
	database, err := store.Open(startupContext, configuration.DBPath, time.Now)
	if err != nil {
		return fmt.Errorf("retrom/main: %w", err)
	}
	defer func() { cleanup.Error("close", database.Close()) }()
	if err := dependencySet.Bootstrap(startupContext, database.SQL, time.Now()); err != nil {
		return fmt.Errorf("bootstrap dependency records: %w", err)
	}
	if err := database.IntegrityCheck(startupContext); err != nil {
		return fmt.Errorf("retrom/main: %w", err)
	}
	blobs, err := blobstore.Open(configuration.DataDir)
	if err != nil {
		return fmt.Errorf("open blob store: %w", err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(configuration.DataDir)
	if err != nil {
		return fmt.Errorf("load launch credentials: %w", err)
	}
	catalogContext, cancelCatalogs := context.WithCancel(context.Background())
	defer cancelCatalogs()
	go func() {
		if catalogErr := dependencySet.BootstrapCatalogs(catalogContext, database.SQL, time.Now()); catalogErr != nil {
			slog.Error("background DAT indexing failed", "error", catalogErr)
			return
		}
		slog.Info("background DAT indexing complete")
	}()

	server := &http.Server{
		Addr: configuration.HTTPAddr,
		Handler: httpapi.New(configuration, database.SQL, dependencySet, blobs, credentials, time.Now).
			Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       75 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	serveErrors := make(chan error, 1)
	go func() {
		slog.Info(
			"retrom HTTP listening",
			"address",
			configuration.HTTPAddr,
			"emulatorjsVersion",
			configuration.ActiveEJSVersion,
		)
		serveErrors <- server.ListenAndServe()
	}()

	stopSignals := make(chan os.Signal, 1)
	signal.Notify(stopSignals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stopSignals)
	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case signalName := <-stopSignals:
		slog.Info("shutdown requested", "signal", signalName.String())
	}

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shutdown HTTP: %w", err)
	}
	return nil
}

func parseServeMode(arguments []string) (config.Mode, error) {
	var value string
	switch {
	case len(arguments) == 1 && strings.HasPrefix(arguments[0], "--mode="):
		value = strings.TrimPrefix(arguments[0], "--mode=")
	case len(arguments) == 2 && arguments[0] == "--mode":
		value = arguments[1]
	default:
		return "", errCommand
	}
	mode, err := config.ParseMode(value)
	if err != nil {
		return "", errCommand
	}
	return mode, nil
}
