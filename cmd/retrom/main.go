package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	dbapi "retrom/internal/database"

	"retrom/internal/composition"

	providerpersistence "retrom/internal/persistence/runtimeprovider"
	providerservice "retrom/internal/service/runtimeprovider"

	dependencypersistence "retrom/internal/persistence/dependencies"
	dependencyservice "retrom/internal/service/dependencies"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/config"
	"retrom/internal/core/scummvm"
	"retrom/internal/dependencies"
	"retrom/internal/filestore"
	"retrom/internal/httpapi"
	"retrom/internal/importing"
	platformpersistence "retrom/internal/persistence/platforminstance"
	"retrom/internal/processlock"
	retromruntime "retrom/internal/runtime"
	runtimeprovider "retrom/internal/runtime/provider"
	"retrom/internal/service/accounts"
	"retrom/internal/service/platforminstance"
	"retrom/internal/store"
)

var (
	errCommand    = errors.New("COMMAND_INVALID")
	errProduction = errors.New("PRODUCTION_PROVIDER_REQUIRED")
)

func main() {
	if err := execute(os.Args[1:]); err != nil {
		slog.Error("retrom stopped", "error", err)
		os.Exit(1)
	}
}

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
		mode, err := parseServeMode(arguments)
		if err != nil {
			return err
		}
		return run(mode)
	}
	return errCommand
}

// Process bootstrap branches are independent fail-fast checks kept in startup order.
func run(mode config.Mode) error {
	configuration, err := loadServerConfiguration(mode)
	if err != nil {
		return err
	}
	startupContext, cancelStartup := context.WithTimeout(
		context.Background(), configuration.StartupCheckTimeout,
	)
	defer cancelStartup()
	resources, err := bootstrapServerResources(startupContext, configuration)
	if err != nil {
		return err
	}
	defer resources.close()
	accountService, err := initializeAccountService(
		startupContext, configuration, resources,
	)
	if err != nil {
		return err
	}
	cancelCatalogs := startCatalogBootstrap(resources)
	defer cancelCatalogs()
	apiServer := httpapi.New(
		configuration, resources.database.SQL, resources.dependencies, resources.blobs,
		resources.credentials, accountService, accountService, time.Now, resources.scummVMDetector,
	).WithReadinessDatabase(resources.database.ReadOnly)
	apiServer.WithRuntimeProvider(
		resources.runtimeProviders.Builder,
		resources.runtimeProviders.Handler,
	)
	defer apiServer.Close()
	return serveHTTP(configuration, apiServer)
}

func loadServerConfiguration(mode config.Mode) (config.Config, error) {
	configuration, err := config.Load(mode)
	if err != nil {
		return config.Config{}, fmt.Errorf("load configuration: %w", err)
	}
	level := slog.LevelInfo
	if err := level.UnmarshalText([]byte(configuration.LogLevel)); err != nil {
		return config.Config{}, fmt.Errorf("parse log level: %w", err)
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(
		os.Stdout, &slog.HandlerOptions{Level: level},
	)))
	return configuration, nil
}

type serverResources struct {
	lock             *processlock.Lock
	dependencies     *dependencies.Set
	database         *store.DB
	blobs            *filestore.Store
	credentials      *retromruntime.Credentials
	runtimeProviders runtimeprovider.Installation
	scummVMDetector  *scummvm.Detector
}

func (resources *serverResources) close() {
	if resources.database != nil {
		cleanup.Error("close", resources.database.Close())
	}
	if resources.lock != nil {
		if err := resources.lock.Close(); err != nil {
			slog.Error("release data root lock", "error", err)
		}
	}
}

func bootstrapServerResources(
	ctx context.Context,
	configuration config.Config,
) (serverResources, error) {
	var result serverResources
	succeeded := false
	defer func() {
		if !succeeded {
			result.close()
		}
	}()
	lock, err := processlock.Acquire(configuration.DataDir)
	if err != nil {
		return result, fmt.Errorf("retrom/main: %w", err)
	}
	result.lock = lock
	result.dependencies, err = dependencies.Load(
		configuration.DependencyRoot,
		configuration.DependencyVersions,
		configuration.ActiveEJSVersion,
	)
	if err != nil {
		return result, fmt.Errorf("verify dependencies: %w", err)
	}
	result.runtimeProviders, err = runtimeprovider.LoadInstallation(runtimeprovider.Paths{
		ActivePath: configuration.ProviderActivePath, InstalledRoot: configuration.ProviderInstalledRoot,
		CatalogPath: configuration.RuntimeTargetCatalogPath, DevRoot: configuration.ProviderDevRoot,
	})
	if err != nil {
		return result, fmt.Errorf("verify runtime provider installation: %w", err)
	}
	if err := validateRuntimeProviderSource(configuration, result.runtimeProviders); err != nil {
		return result, fmt.Errorf("verify runtime provider installation: %w", err)
	}
	result.scummVMDetector, err = result.runtimeProviders.ScummVMDetector(
		filepath.Join(configuration.DataDir, "runtime-tools", "scummvm"),
	)
	if err != nil && !errors.Is(err, runtimeprovider.ErrScummVMNotInstalled) {
		return result, fmt.Errorf("prepare ScummVM detector: %w", err)
	}
	if err := openAndBootstrapDatabase(ctx, configuration, &result); err != nil {
		return result, err
	}
	result.blobs, err = filestore.Open(configuration.DataDir)
	if err != nil {
		return result, fmt.Errorf("open blob store: %w", err)
	}
	result.credentials, err = retromruntime.LoadOrCreateCredentials(configuration.DataDir)
	if err != nil {
		return result, fmt.Errorf("load launch credentials: %w", err)
	}

	succeeded = true
	return result, nil
}

func validateRuntimeProviderSource(configuration config.Config, installation runtimeprovider.Installation) error {
	if configuration.Mode == config.ModeRelease && installation.Active.Source != "production" {
		return errProduction
	}
	return nil
}

func openAndBootstrapDatabase(
	ctx context.Context,
	configuration config.Config,
	resources *serverResources,
) error {
	database, err := store.Open(ctx, configuration.DBPath, time.Now)
	if err != nil {
		return fmt.Errorf("retrom/main: %w", err)
	}
	resources.database = database
	providers := providerservice.New(providerpersistence.New(database.SQL))
	if err := providers.Reconcile(ctx, resources.runtimeProviders.Projection, time.Now()); err != nil {
		return fmt.Errorf("reconcile runtime providers: %w", err)
	}
	dependencies := dependencyservice.New(resources.dependencies, dependencypersistence.New(database.SQL))
	if err := dependencies.Bootstrap(ctx, time.Now()); err != nil {
		return fmt.Errorf("bootstrap dependency records: %w", err)
	}
	if err := platforminstance.New(platformpersistence.New(database.SQL), time.Now).ValidateCatalog(ctx); err != nil {
		return fmt.Errorf("validate recommended game directories: %w", err)
	}
	if err := database.IntegrityCheck(ctx); err != nil {
		return fmt.Errorf("retrom/main: %w", err)
	}
	return nil
}

func initializeAccountService(
	ctx context.Context,
	configuration config.Config,
	resources serverResources,
) (*accounts.Service, error) {
	blocklist, err := authn.LoadBlocklist(configuration.DependencyRoot)
	if err != nil {
		return nil, fmt.Errorf("load password blocklist: %w", err)
	}
	accountService, err := composition.NewAccounts(
		ctx, resources.database.SQL, resources.credentials,
		configuration.Mode, blocklist, time.Now,
	)
	if err != nil {
		return nil, fmt.Errorf("initialize account service: %w", err)
	}
	if err := accountService.Start(ctx); err != nil {
		return nil, fmt.Errorf("validate account state: %w", err)
	}
	return accountService, nil
}

func startCatalogBootstrap(resources serverResources) context.CancelFunc {
	catalogContext, cancel := context.WithCancel(context.Background())
	go bootstrapCatalogs(catalogContext, resources.dependencies, resources.database.SQL)
	return cancel
}

func bootstrapCatalogs(ctx context.Context, dependencySet *dependencies.Set, database dbapi.DB) {
	dependencies := dependencyservice.New(dependencySet, dependencypersistence.New(database))
	if err := dependencies.BootstrapCatalogs(ctx, time.Now()); err != nil {
		slog.Error("background DAT indexing failed", "error", err)
		return
	}
	slog.Info("background DAT indexing complete")
}

func serveHTTP(configuration config.Config, apiServer *httpapi.Server) error {
	server := &http.Server{
		Addr: configuration.HTTPAddr, Handler: apiServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute,
		WriteTimeout: 2 * time.Minute, IdleTimeout: 75 * time.Second, MaxHeaderBytes: 64 << 10,
	}
	serveErrors := make(chan error, 1)
	go func() {
		slog.Info("retrom HTTP listening")
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
