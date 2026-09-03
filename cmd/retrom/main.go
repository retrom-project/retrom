package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"retrom/internal/accounts"
	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/config"
	"retrom/internal/dependencies"
	"retrom/internal/httpapi"
	"retrom/internal/importing"
	"retrom/internal/maintenance"
	"retrom/internal/netplay"
	"retrom/internal/platforminstance"
	"retrom/internal/processlock"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/runtimeprovider"
	"retrom/internal/store"
)

var (
	errBackupArgument  = errors.New("BACKUP_ARGUMENT_INVALID")
	errRestoreArgument = errors.New("RESTORE_ARGUMENT_INVALID")
	errSetupArgument   = errors.New("SETUP_CODE_ARGUMENT_INVALID")
	errAdminArgument   = errors.New("ADMIN_RESET_ARGUMENT_INVALID")
	errCommand         = errors.New("COMMAND_INVALID")
	errTerminal        = errors.New("TERMINAL_DESCRIPTOR_INVALID")
)

func main() {
	if err := execute(os.Args[1:]); err != nil {
		slog.Error("retrom stopped", "error", err)
		os.Exit(1)
	}
}

func execute(arguments []string) error {
	return executeWithPasswordReader(arguments, readPasswordFromTTY)
}

// Each CLI command owns an explicit argument contract.
func executeWithPasswordReader(arguments []string, readPassword func(string) (string, error)) error {
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
	switch arguments[0] {
	case "setup-code":
		return executeSetupCode(arguments[1:])
	case "admin-reset":
		return executeAdminReset(arguments[1:], readPassword)
	case "backup":
		return executeBackup(arguments[1:])
	case "restore":
		return executeRestore(arguments[1:])
	default:
		return errCommand
	}
}

func executeSetupCode(arguments []string) error {
	flags := flag.NewFlagSet("retrom setup-code", flag.ContinueOnError)
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return errSetupArgument
	}
	configuration, err := config.LoadBackupMaintenance()
	if err != nil {
		return fmt.Errorf("retrom/main: %w", err)
	}
	code, err := readSetupCode(context.Background(), configuration)
	if err != nil {
		return fmt.Errorf("retrom/main: %w", err)
	}
	if _, err := fmt.Fprintln(os.Stdout, code); err != nil {
		return fmt.Errorf("write setup code: %w", err)
	}
	return nil
}

func executeAdminReset(arguments []string, readPassword func(string) (string, error)) error {
	flags := flag.NewFlagSet("retrom admin-reset", flag.ContinueOnError)
	username := flags.String("username", "", "existing non-deleted administrator username")
	if err := flags.Parse(arguments); err != nil || *username == "" || flags.NArg() != 0 {
		return errAdminArgument
	}
	configuration, err := config.LoadBackupMaintenance()
	if err != nil {
		return fmt.Errorf("retrom/main: %w", err)
	}
	if err := resetOfflineAdmin(context.Background(), configuration, *username, readPassword); err != nil {
		return fmt.Errorf("retrom/main: %w", err)
	}
	return writeCommandResult(map[string]any{"status": "admin_reset_complete", "username": *username})
}

func executeBackup(arguments []string) error {
	flags := flag.NewFlagSet("retrom backup", flag.ContinueOnError)
	output := flags.String("output", "", "absolute path for a new backup bundle")
	if err := flags.Parse(arguments); err != nil || *output == "" || flags.NArg() != 0 {
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
	return writeCommandResult(map[string]any{
		"status": "backup_complete", "output": *output, "fileCount": manifest.Counts.FileCount,
	})
}

func executeRestore(arguments []string) error {
	flags := flag.NewFlagSet("retrom restore", flag.ContinueOnError)
	input := flags.String("input", "", "absolute path to a backup bundle")
	output := flags.String("output-data-dir", "", "absolute path for a new data root")
	if err := flags.Parse(arguments); err != nil || *input == "" || *output == "" || flags.NArg() != 0 {
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
	return writeCommandResult(map[string]any{
		"status": "restore_complete", "outputDataDir": *output,
		"requiredDependencyVersions":      manifest.DependencyVersions,
		"requiredActiveEmulatorjsVersion": manifest.ActiveEmulatorjsVersion,
	})
}

func readSetupCode(ctx context.Context, configuration config.Maintenance) (string, error) {
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(configuration.DBPath)+"?mode=ro")
	if err != nil {
		return "", fmt.Errorf("open setup-code database: %w", err)
	}
	defer func() { cleanup.Error("close", database.Close()) }()
	credentials, err := retromruntime.LoadCredentials(configuration.DataDir)
	if err != nil {
		return "", fmt.Errorf("load setup-code credentials: %w", err)
	}
	code, err := accounts.ReadSetupCode(ctx, database, credentials)
	if err != nil {
		return "", fmt.Errorf("derive setup code: %w", err)
	}
	return code, nil
}

func resetOfflineAdmin(
	ctx context.Context,
	configuration config.Maintenance,
	username string,
	readPassword func(string) (string, error),
) error {
	lock, err := processlock.Acquire(configuration.DataDir)
	if err != nil {
		return fmt.Errorf("acquire offline recovery lock: %w", err)
	}
	defer func() { cleanup.Error("close", lock.Close()) }()
	password, err := readPassword("New password: ")
	if err != nil {
		return fmt.Errorf("read offline recovery password: %w", err)
	}
	confirmation, err := readPassword("Confirm new password: ")
	if err != nil {
		return fmt.Errorf("read offline recovery confirmation: %w", err)
	}
	database, err := store.Open(ctx, configuration.DBPath, time.Now)
	if err != nil {
		return fmt.Errorf("open offline recovery database: %w", err)
	}
	defer func() { cleanup.Error("close", database.Close()) }()
	credentials, err := retromruntime.LoadCredentials(configuration.DataDir)
	if err != nil {
		return fmt.Errorf("load offline recovery credentials: %w", err)
	}
	blocklist, err := authn.LoadBlocklist(configuration.DependencyRoot)
	if err != nil {
		return fmt.Errorf("load offline recovery password blocklist: %w", err)
	}
	accountService, err := accounts.New(
		ctx, database.SQL, credentials, config.ModeRelease, blocklist, time.Now,
	)
	if err != nil {
		return fmt.Errorf("initialize offline account service: %w", err)
	}
	if err := accountService.OfflineAdminReset(ctx, username, password, confirmation); err != nil {
		return fmt.Errorf("reset offline administrator: %w", err)
	}
	return nil
}

func readPasswordFromTTY(prompt string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("open controlling terminal: %w", err)
	}
	defer func() { cleanup.Error("close", tty.Close()) }()
	if _, err := fmt.Fprint(tty, prompt); err != nil {
		return "", fmt.Errorf("write password prompt: %w", err)
	}
	descriptor, err := terminalDescriptor(tty)
	if err != nil {
		return "", err
	}
	password, err := term.ReadPassword(descriptor)
	_, _ = fmt.Fprintln(tty)
	if err != nil {
		return "", fmt.Errorf("read terminal password: %w", err)
	}
	return string(password), nil
}

func terminalDescriptor(file *os.File) (int, error) {
	descriptor := file.Fd()
	if uint64(descriptor) > uint64(math.MaxInt) {
		return 0, errTerminal
	}
	return int(descriptor), nil
}

func writeCommandResult(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("encode command result: %w", err)
	}
	return nil
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
	netplayService, accountService, err := initializeRuntimeServices(
		startupContext, configuration, resources,
	)
	if err != nil {
		return err
	}
	cancelCatalogs := startCatalogBootstrap(resources)
	defer cancelCatalogs()
	apiServer := httpapi.New(
		configuration, resources.database.SQL, resources.dependencies, resources.blobs,
		resources.credentials, accountService, accountService, time.Now,
	).WithReadinessDatabase(resources.database.ReadOnly).WithNetplay(netplayService)
	apiServer.WithRuntimeProvider(
		resources.runtimeProviders.Catalog,
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
	lock               *processlock.Lock
	dependencies       *dependencies.Set
	database           *store.DB
	blobs              *blobstore.Store
	credentials        *retromruntime.Credentials
	netplayRegistry    *netplay.Registry
	netplayCredentials *netplay.Credentials
	runtimeProviders   runtimeprovider.Installation
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
		CatalogPath: configuration.RuntimeTargetCatalogPath,
	})
	if err != nil {
		return result, fmt.Errorf("verify runtime provider installation: %w", err)
	}
	result.database, err = store.Open(ctx, configuration.DBPath, time.Now)
	if err != nil {
		return result, fmt.Errorf("retrom/main: %w", err)
	}
	if err := result.runtimeProviders.Reconcile(ctx, result.database.SQL, time.Now()); err != nil {
		return result, fmt.Errorf("reconcile runtime providers: %w", err)
	}
	if err := result.dependencies.Bootstrap(ctx, result.database.SQL, time.Now()); err != nil {
		return result, fmt.Errorf("bootstrap dependency records: %w", err)
	}
	if err := platforminstance.New(result.database.SQL, time.Now).ValidateCatalog(ctx); err != nil {
		return result, fmt.Errorf("validate recommended game directories: %w", err)
	}
	if err := result.database.IntegrityCheck(ctx); err != nil {
		return result, fmt.Errorf("retrom/main: %w", err)
	}
	result.blobs, err = blobstore.Open(configuration.DataDir)
	if err != nil {
		return result, fmt.Errorf("open blob store: %w", err)
	}
	result.credentials, err = retromruntime.LoadOrCreateCredentials(configuration.DataDir)
	if err != nil {
		return result, fmt.Errorf("load launch credentials: %w", err)
	}
	result.netplayRegistry, err = netplay.LoadRegistry(
		configuration.DependencyRoot, result.dependencies,
	)
	if err != nil {
		return result, fmt.Errorf("load netplay registry: %w", err)
	}
	result.netplayCredentials, err = netplay.LoadOrCreateCredentials(configuration.DataDir)
	if err != nil {
		return result, fmt.Errorf("load netplay credentials: %w", err)
	}
	succeeded = true
	return result, nil
}

func initializeRuntimeServices(
	ctx context.Context,
	configuration config.Config,
	resources serverResources,
) (*netplay.Service, *accounts.Service, error) {
	netplayService := netplay.NewService(
		resources.database.SQL, resources.netplayRegistry, resources.netplayCredentials,
		netplay.Options{
			MaxActiveRooms: configuration.NetplayMaxActiveRooms,
			DraftIdle:      configuration.NetplayRoomIdleDraft,
			WaitingIdle:    configuration.NetplayRoomIdleWaiting,
			ReconnectLease: configuration.NetplayReconnectLease,
		}, time.Now,
	)
	if err := netplayService.Recover(ctx, "SERVER_RESTARTED"); err != nil {
		return nil, nil, fmt.Errorf("recover netplay state: %w", err)
	}
	blocklist, err := authn.LoadBlocklist(configuration.DependencyRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("load password blocklist: %w", err)
	}
	accountService, err := accounts.New(
		ctx, resources.database.SQL, resources.credentials,
		configuration.Mode, blocklist, time.Now,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("initialize account service: %w", err)
	}
	if err := accountService.Start(ctx); err != nil {
		return nil, nil, fmt.Errorf("validate account state: %w", err)
	}
	return netplayService, accountService, nil
}

func startCatalogBootstrap(resources serverResources) context.CancelFunc {
	catalogContext, cancel := context.WithCancel(context.Background())
	go bootstrapCatalogs(catalogContext, resources.dependencies, resources.database.SQL)
	return cancel
}

func bootstrapCatalogs(ctx context.Context, dependencySet *dependencies.Set, database *sql.DB) {
	if err := dependencySet.BootstrapCatalogs(ctx, database, time.Now()); err != nil {
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
