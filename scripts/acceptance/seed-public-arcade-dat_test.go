package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestSmokeDatabaseWaitsForBoundedAcceptanceWriterContention(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "retrom.db")
	database, err := openSmokeDatabase(ctx, databasePath)
	if err != nil {
		t.Fatalf("open smoke database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := database.Close(); closeErr != nil {
			t.Errorf("close smoke database: %v", closeErr)
		}
	})
	var timeoutMS int
	if err := database.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeoutMS); err != nil {
		t.Fatalf("read busy timeout: %v", err)
	}
	if timeoutMS != acceptanceSQLiteBusyTimeoutMS {
		t.Fatalf("busy timeout = %d, want %d", timeoutMS, acceptanceSQLiteBusyTimeoutMS)
	}
	if _, err := database.ExecContext(ctx, "CREATE TABLE contention(value INTEGER NOT NULL)"); err != nil {
		t.Fatalf("create contention table: %v", err)
	}
	blocker, err := openSmokeDatabase(ctx, databasePath)
	if err != nil {
		t.Fatalf("open blocking database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := blocker.Close(); closeErr != nil {
			t.Errorf("close blocking database: %v", closeErr)
		}
	})
	connection, err := blocker.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire blocking connection: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := connection.Close(); closeErr != nil {
			t.Errorf("close blocking connection: %v", closeErr)
		}
	})
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("begin blocking write: %v", err)
	}
	if _, err := connection.ExecContext(ctx, "INSERT INTO contention(value) VALUES(1)"); err != nil {
		t.Fatalf("hold blocking write: %v", err)
	}
	released := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		_, commitErr := connection.ExecContext(ctx, "COMMIT")
		released <- commitErr
	}()
	started := time.Now()
	if _, err := database.ExecContext(ctx, "INSERT INTO contention(value) VALUES(2)"); err != nil {
		t.Fatalf("write did not wait for bounded contention: %v", err)
	}
	if elapsed := time.Since(started); elapsed < 50*time.Millisecond || elapsed > 5*time.Second {
		t.Fatalf("contention wait = %s, want between 50ms and 5s", elapsed)
	}
	if err := <-released; err != nil {
		t.Fatalf("release blocking write: %v", err)
	}
}

func TestSmokeFixtureAllowlistLoadsEveryLockedCatalog(t *testing.T) {
	t.Parallel()
	for fixtureID, fixture := range smokeFixtures {
		t.Run(fixtureID, func(t *testing.T) {
			t.Parallel()
			catalog, digest, path, err := loadSmokeCatalog(context.Background(), fixture)
			if err != nil || digest != fixture.SHA256 || filepath.Base(path) != filepath.Base(fixture.RelativePath) || len(catalog.Machines) != len(fixture.Machines) {
				t.Fatalf("load fixture = %q %q %d, error=%v", digest, path, len(catalog.Machines), err)
			}
		})
	}
}

func TestSmokeFixtureAllowlistRejectsUnknownAndDrift(t *testing.T) {
	t.Parallel()
	if err := run(context.Background(), "unused.db", "unknown"); !errors.Is(err, errUnsupportedSmokeFixture) {
		t.Fatalf("unknown fixture error = %v", err)
	}
	fixture := smokeFixtures["fbalpha2012_cps1"]
	fixture.SHA256 = "0" + fixture.SHA256[1:]
	if _, _, _, err := loadSmokeCatalog(context.Background(), fixture); err == nil {
		t.Fatal("drifted fixture digest accepted")
	}
	fixture = smokeFixtures["fbalpha2012_cps1"]
	fixture.Machines = []string{"other"}
	if _, _, _, err := loadSmokeCatalog(context.Background(), fixture); err == nil {
		t.Fatal("drifted fixture machine set accepted")
	}
}
