package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"retrom/internal/authn"
	"retrom/internal/httpapi"
	"retrom/internal/persistence"
	"retrom/internal/runtimeclient"
	"retrom/internal/service/accounts"
	"retrom/internal/service/bios"
	"retrom/internal/service/directory"
	"retrom/internal/service/favorites"
	"retrom/internal/service/files"
	"retrom/internal/service/home"
	"retrom/internal/service/library"
	"retrom/internal/service/recent"
	"retrom/internal/service/runs"
	"retrom/internal/service/saves"
	"retrom/internal/service/scans"
	"retrom/internal/service/tags"
	"retrom/internal/storage"
	"retrom/internal/temporary"
)

func run(ctx context.Context) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	repository, err := openRepository(ctx)
	if err != nil {
		return err
	}
	defer repository.Close()
	blocklist, err := authn.LoadBlocklist(environment("RETROM_DEPENDENCY_ROOT", "data"))
	if err != nil {
		return fmt.Errorf("load password blocklist: %w", err)
	}
	redisClient, err := temporary.New(environment("RETROM_REDIS_ADDR", "127.0.0.1:6379"),
		"retrom:"+environment("RETROM_PFB_ID", "development")+":")
	if err != nil {
		return fmt.Errorf("initialize temporary store: %w", err)
	}
	defer func() {
		if closeErr := redisClient.Close(); closeErr != nil {
			slog.Error("close Redis", "error", closeErr)
		}
	}()
	service := &accounts.Service{
		Repository: repository, Redis: redisClient,
		Hasher: authn.NewPasswordHasher(), Blocklist: blocklist, Now: time.Now,
	}

	if environment("RETROM_MODE", "release") == "test" {
		if err = service.BootstrapTest(ctx); err != nil {
			return fmt.Errorf("bootstrap test account: %w", err)
		}
	}
	dataRoot := environment("RETROM_DATA_DIR", ".pfb/workspace/data")
	managed, err := storage.Open(dataRoot + "/managed")
	if err != nil {
		return fmt.Errorf("initialize managed storage: %w", err)
	}
	defer func() {
		if closeErr := managed.Close(); closeErr != nil {
			slog.Error("close managed storage", "error", closeErr)
		}
	}()
	runtime,
		err := runtimeclient.Open(ctx,
		environment("RETROM_RUNTIME_ROOT",
			"../retrom-runtime"),
		environment("RETROM_NODE",
			"node"),
		environment("RETROM_PROVIDER_ROOT",
			".pfb/workspace/providers"))
	if err != nil {
		return fmt.Errorf("initialize runtime facts: %w", err)
	}
	defer runtime.Close()
	runtime.Locate = managed.Absolute
	origin := environment("RETROM_PUBLIC_ORIGIN", "http://localhost:4000")
	service.LinkKey, err = linkKey(dataRoot)
	if err != nil {
		return err
	}
	service.PublicOrigin = origin
	runService := &runs.Service{
		Storage:    managed,
		Repository: repository,
		Runtime:    runtime,
		Redis:      redisClient,
		Now:        time.Now,
		Origin:     origin,
		IsolationTemplate: environment("RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE",
			"http://{runId}.rpg.localhost:4000"),
	}
	saveService := &saves.Service{Repository: repository, Runs: runService, Storage: managed, Now: time.Now}
	sources, err := sourceRoots()
	if err != nil {
		return err
	}
	proxies, err := trustedProxies()
	if err != nil {
		return err
	}
	if err = repository.InterruptScans(ctx, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("interrupt old scans: %w", err)
	}
	scanService := &scans.Service{
		Repository: repository,
		Runtime:    runtime,
		Storage:    managed,
		Sources:    storage.Sources{Roots: sources},
		Now:        time.Now,
		Context:    ctx,
	}
	transport := &httpapi.Server{
		Accounts: service, Origin: environment("RETROM_PUBLIC_ORIGIN", "http://localhost:4000"),
		CookieName: "retrom_session", TrustedProxies: proxies,
		Runtime: runtime, Storage: managed, Sources: storage.Sources{Roots: sources},
		WebRoot:   environment("RETROM_WEB_ROOT", "web"),
		Directory: &directory.Service{Repository: repository, Catalog: runtime.GetCatalog, Now: time.Now},
		Library: &library.Service{
			Repository: repository, Storage: managed, Runtime: runtime,
			ValidateConfig: runtime.Validate, Now: time.Now,
		},
		Tags: &tags.Service{
			Repository: repository,
			Now:        time.Now,
		},
		Favorites: &favorites.Service{
			Repository: repository,
			Now:        time.Now,
		},

		Recent: &recent.Service{Repository: repository}, Runs: runService, Saves: saveService,
		Bios:  &bios.Service{Repository: repository, Runtime: runtime, Storage: managed, Now: time.Now},
		Scans: scanService,
		Home:  &home.Service{Repository: repository, Saves: saveService},
	}
	defer func() { stop(); scanService.Wait() }()
	cleaner := &files.Service{Repository: repository, Storage: managed, Now: time.Now, Grace: 24 * time.Hour}
	drained := make(chan struct{})
	go func() { defer close(drained); cleaner.Run(ctx) }()
	defer func() { stop(); <-drained }()
	handler, err := transport.Handler()
	if err != nil {
		return fmt.Errorf("initialize HTTP contract: %w", err)
	}
	server := &http.Server{
		Addr: environment("RETROM_HTTP_ADDR", "127.0.0.1:8080"), Handler: handler,
		BaseContext: func(net.Listener) context.Context { return ctx },
		ReadTimeout: 10 * time.Minute, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second,
	}
	return serve(ctx, server)
}

func serve(ctx context.Context, server *http.Server) error {
	stopped := make(chan error, 1)
	go func() { stopped <- server.ListenAndServe() }()
	select {
	case err := <-stopped:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		deadline, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(deadline); err != nil {
			return fmt.Errorf("shutdown HTTP: %w", err)
		}
		return nil
	}
}

func environment(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func openRepository(ctx context.Context) (*persistence.Repository, error) {
	repository, err := persistence.Open(ctx, os.Getenv("RETROM_DATABASE_URL"))
	if err != nil {
		return nil, fmt.Errorf("initialize database: %w", err)
	}
	if err = repository.Migrate(ctx); err != nil {
		repository.Close()
		return nil, fmt.Errorf("initialize schema: %w", err)
	}
	return repository, nil
}
