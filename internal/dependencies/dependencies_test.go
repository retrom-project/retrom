//go:build integration

package dependencies

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/store"
)

func TestBootstrapMaterializedDependencies(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test file")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	manifest, err := Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3", "4.3.0-pre"}, "4.2.3")
	if err != nil {
		t.Fatalf("load dependencies: %v", err)
	}

	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })

	if err := manifest.Bootstrap(context.Background(), database.SQL, time.Now()); err != nil {
		t.Fatalf("bootstrap dependencies: %v", err)
	}

	var activeArtifacts int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT count(*)
FROM core_artifacts
WHERE enabled = 1
`).Scan(&activeArtifacts); err != nil {
		t.Fatalf("count active artifacts: %v", err)
	}
	if activeArtifacts != 28 {
		t.Fatalf("active artifacts = %d, want 28", activeArtifacts)
	}
	var dosVersion string
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT emulatorjs_version
FROM core_artifacts
WHERE core_id='dosbox_pure' AND enabled=1
`).Scan(&dosVersion); err != nil || dosVersion != "4.3.0-pre" {
		t.Fatalf("active DOS artifact = %q, error=%v", dosVersion, err)
	}
	var oldDOSActive int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT count(*)
FROM core_artifacts
WHERE core_id='dosbox_pure' AND emulatorjs_version='4.2.3' AND enabled=1
`).Scan(&oldDOSActive); err != nil || oldDOSActive != 0 {
		t.Fatalf("old DOS artifact active = %d, error=%v", oldDOSActive, err)
	}

	var pendingDATs, activeDATs int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT sum(CASE WHEN parse_status='PENDING' THEN 1 ELSE 0 END),
sum(is_active)
FROM dat_versions
WHERE source='BUILTIN'
`).Scan(&pendingDATs, &activeDATs); err != nil {
		t.Fatalf("count pending DATs: %v", err)
	}
	if pendingDATs != 3 || activeDATs != 0 {
		t.Fatalf("pre-index DATs pending/active = %d/%d, want 3/0", pendingDATs, activeDATs)
	}
	var staticBIOS, matchedCatalog int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT count(*),
sum(CASE WHEN length(catalog_digest)=64 THEN 1 ELSE 0 END)
FROM bios_requirements
WHERE source_kind='STATIC'
AND enabled=1
`).Scan(&staticBIOS, &matchedCatalog); err != nil {
		t.Fatalf("count BIOS requirements: %v", err)
	}
	if staticBIOS != 22 || matchedCatalog != staticBIOS {
		t.Fatalf("static BIOS catalog = %d/%d, want 22/22", staticBIOS, matchedCatalog)
	}
	var externalBIOS int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT count(*)
FROM bios_requirements
WHERE delivery_kind='EXTERNAL_FILE'
AND core_id='melonds'
AND emulator_path IN ('/retroarch/userdata/system/bios7.bin',
'/retroarch/userdata/system/bios9.bin',
'/retroarch/userdata/system/firmware.bin')
`).Scan(&externalBIOS); err != nil || externalBIOS != 3 {
		t.Fatalf("melonDS external BIOS = %d, error=%v", externalBIOS, err)
	}
	var compatibility string
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT compatibility_config_json
FROM core_artifacts
WHERE core_id='ppsspp' AND enabled=1
`).Scan(&compatibility); err != nil || !strings.Contains(compatibility, `"persistentSaveMode":"NONE"`) ||
		!strings.Contains(compatibility, `"requestedArtifactBasename":"ppsspp-thread-wasm.data"`) {
		t.Fatalf("PPSSPP compatibility = %s, error=%v", compatibility, err)
	}

	// Bootstrap is intentionally idempotent; every process start verifies the
	// same selected release without creating duplicate rows.
	if err := manifest.Bootstrap(context.Background(), database.SQL, time.Now()); err != nil {
		t.Fatalf("repeat bootstrap: %v", err)
	}
	var advanced int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT count(*)
FROM bios_requirements
WHERE version != 1
`).Scan(&advanced); err != nil ||
		advanced != 0 {
		t.Fatalf("idempotent BIOS versions advanced = %d, error=%v", advanced, err)
	}
}

func TestBIOSActivationOptionsRejectConflictingSeed(t *testing.T) {
	t.Parallel()
	catalog := []staticBIOS{
		{coreID: "mgba", logical: "gba_bios.bin", options: `{"mgba_use_bios":"ON"}`},
		{coreID: "mgba", logical: "gb_bios.bin", options: `{"mgba_use_bios":"OFF"}`},
	}
	if err := validateBIOSActivationOptions(catalog); err == nil {
		t.Fatal("conflicting BIOS activation option seed was accepted")
	}
}
