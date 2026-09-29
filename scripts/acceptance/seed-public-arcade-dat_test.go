package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbapi "retrom/internal/database"
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
	if err := dbapi.QueryRowContext(ctx, database, "PRAGMA busy_timeout").Scan(&timeoutMS); err != nil {
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
	transaction, err := blocker.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin blocking write: %v", err)
	}
	t.Cleanup(func() { dbapi.Rollback(transaction) })
	if _, err := transaction.ExecContext(ctx, "INSERT INTO contention(value) VALUES(1)"); err != nil {
		t.Fatalf("hold blocking write: %v", err)
	}
	released := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		released <- transaction.Commit()
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

func TestMAMECandidateDATMustMatchItsLinkedFamilyRosterAndDigest(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	buildID := strings.Repeat("a", 64)
	metadata, err := json.Marshal(map[string]any{
		"schemaVersion": 1, "adapterAbi": "retrom-mame-dylink-v1", "buildId": buildID,
		"families": map[string]any{"pacman": map[string]any{
			"arcade": true, "module": "mame-pacman.wasm", "machines": []string{"___empty", "pacman"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	dat := []byte(`<?xml version="1.0"?><mame retromBuildId="` + buildID + `"><machine name="pacman"><description>Pac-Man</description></machine></mame>`)
	files := []map[string]any{}
	for name, data := range map[string][]byte{"mame-build.json": metadata, "mame-arcade.xml": dat, "mame-pacman.wasm": {0}} {
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		files = append(files, map[string]any{"filename": name, "sizeBytes": len(data), "sha256": hex.EncodeToString(digest[:])})
	}
	descriptor, err := json.Marshal(map[string]any{
		"schemaVersion": 1, "kind": "RETROM_CORE_CANDIDATE_V1", "coreId": "mame",
		"adapterAbi": "retrom-mame-dylink-v1", "files": files,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "retrom-core-candidate.json"), descriptor, 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, _, _, err := loadMAMECandidateCatalog(context.Background(), root)
	if err != nil || len(catalog.Machines) != 1 || catalog.Machines[0].Name != "pacman" {
		t.Fatalf("candidate catalog = %+v, error=%v", catalog, err)
	}
	if err := os.WriteFile(filepath.Join(root, "mame-arcade.xml"), append(dat, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := loadMAMECandidateCatalog(context.Background(), root); !errors.Is(err, errSmokeFixtureDigestDrift) {
		t.Fatalf("tampered candidate error = %v", err)
	}
}
