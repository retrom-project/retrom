//go:build integration

package dependencies

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/dependencies"
	service "retrom/internal/service/dependencies"
	"retrom/internal/store"
	"retrom/internal/testsupport"
)

func TestPairedDATRejectsMismatchBeforeRetiringCurrentCatalog(t *testing.T) {
	ctx := context.Background()
	fixture := filepath.Join("..", "..", "..", "testdata", "public-roms", "arcade-smoke", "fbneo", "fbneo-smoke.dat")
	contents, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	sha := hex.EncodeToString(digest[:])
	set, err := dependencies.Load(filepath.Join("..", "..", "..", "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	// This test isolates paired catalog publication from unrelated built-in DATs.
	for _, version := range set.Versions {
		for i := range version.Manifest.Cores {
			version.Manifest.Cores[i].DAT = nil
		}
	}
	if err := set.UsePairedDAT(ctx, "emulatorjs", "fbneo", fixture, sha, strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	if err := testsupport.SeedPlatformInstances(ctx, database.SQL); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `UPDATE runtime_targets
SET manifest_fragment_json=json_set(manifest_fragment_json,'$.arcadeDAT.asset.sha256',?)
WHERE provider_id='emulatorjs' AND target_id='fbneo'`, sha); err != nil {
		t.Fatal(err)
	}
	app := service.New(set, New(database.SQL))
	if err := app.Bootstrap(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := app.BootstrapCatalogs(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	paired := set.Versions[set.Order[len(set.Order)-1]]
	paired.Manifest.Cores[0].DAT.SHA256 = strings.Repeat("b", 64)
	if err := app.Bootstrap(ctx, time.Now()); !errors.Is(err, dependencies.ErrInvalid) {
		t.Fatalf("mismatch accepted: %v", err)
	}
	var active string
	if err := dbapi.QueryRowContext(ctx, database.SQL, `SELECT sha256 FROM dat_versions
WHERE provider_id='emulatorjs' AND target_id='fbneo' AND is_active=1`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != sha {
		t.Fatal("mismatched candidate retired the valid pair")
	}
}
