package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestSmokeDatabaseWaitsForBoundedAcceptanceWriterContention(t *testing.T) {
	t.Parallel()
	database, err := openSmokeDatabase(context.Background(), filepath.Join(t.TempDir(), "retrom.db"))
	if err != nil {
		t.Fatalf("open smoke database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := database.Close(); closeErr != nil {
			t.Errorf("close smoke database: %v", closeErr)
		}
	})
	var timeoutMS int
	if err := database.QueryRowContext(context.Background(), "PRAGMA busy_timeout").Scan(&timeoutMS); err != nil {
		t.Fatalf("read busy timeout: %v", err)
	}
	if timeoutMS != acceptanceSQLiteBusyTimeoutMS {
		t.Fatalf("busy timeout = %d, want %d", timeoutMS, acceptanceSQLiteBusyTimeoutMS)
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
