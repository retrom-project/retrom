package application

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"retrom/internal/config"
	dbsqlite "retrom/internal/database/sqlite"
	"retrom/internal/filestore"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/service/metadatascrape"
)

func newLifecycleFixture(t *testing.T) *Services {
	t.Helper()
	// An empty schema proves construction never performs worker recovery.
	db, err := dbsqlite.Open(":memory:", dbsqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	dir := t.TempDir()
	files, err := filestore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(dir)
	if err != nil {
		t.Fatal(err)
	}
	services, err := New(t.Context(), Inputs{
		Config: config.Config{DataDir: dir, PublicOrigin: &url.URL{Scheme: "http", Host: "localhost"}},
		Writer: db, Reader: db, Files: files, Credentials: credentials, Now: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(services.Close)
	return services
}

func TestConstructionDoesNotRecoverAndStartupReportsRecoveryFailure(t *testing.T) {
	services := newLifecycleFixture(t)
	if err := services.Start(t.Context()); err == nil {
		t.Fatal("startup concealed missing worker tables")
	}
	services.Close()
	if err := services.Metadata.Recover(t.Context()); !errors.Is(err, metadatascrape.ErrWorkerClosed) {
		t.Fatalf("metadata worker survived failed startup: %v", err)
	}
	if !services.stopping.Load() {
		t.Fatal("failed startup did not close services")
	}
	if err := services.Start(t.Context()); !errors.Is(err, ErrClosed) {
		t.Fatalf("restart after failure=%v", err)
	}
	services.Close()
}

func TestCancelledStartupPreservesTheCauseAndCanClose(t *testing.T) {
	services := newLifecycleFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := services.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled startup=%v", err)
	}
	services.Close()
	if err := services.Metadata.Recover(t.Context()); !errors.Is(err, metadatascrape.ErrWorkerClosed) {
		t.Fatalf("metadata worker survived failed startup: %v", err)
	}
	if !services.stopping.Load() {
		t.Fatal("failed startup did not close services")
	}
	if err := services.Start(t.Context()); !errors.Is(err, ErrClosed) {
		t.Fatalf("restart after failure=%v", err)
	}
	services.Close()
}

func TestCloseBeforeStartPreventsBackgroundWork(t *testing.T) {
	services := newLifecycleFixture(t)
	services.Close()
	if err := services.Start(t.Context()); !errors.Is(err, ErrClosed) {
		t.Fatalf("start after Close=%v", err)
	}
}

func TestMissingApplicationDependenciesReturnConstructionError(t *testing.T) {
	if services, err := New(t.Context(), Inputs{}); err == nil || services != nil {
		t.Fatalf("missing inputs accepted: %v", err)
	}
}
