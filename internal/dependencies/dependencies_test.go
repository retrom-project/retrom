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
	"retrom/internal/testassert"
)

func TestBootstrapMaterializedDependencies(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	testassert.True(t, ok, "locate test file")
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	manifest, err := Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3", "4.3.0-pre"}, "4.2.3")
	testassert.Falsef(t, err != nil, "load dependencies: %v", err)

	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	testassert.Falsef(t, err != nil, "open database: %v", err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })

	bootstrapTime := time.Now().Truncate(time.Millisecond)
	if err := manifest.Bootstrap(context.Background(), database.SQL, bootstrapTime); err != nil {
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
	testassert.Falsef(t, activeArtifacts != 35, "active artifacts = %d, want 35", activeArtifacts)
	var currentCompatibilityRows int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT count(*)
FROM core_artifacts
WHERE enabled=1
AND json_extract(compatibility_config_json,'$.schemaVersion')=5
`).Scan(&currentCompatibilityRows); err != nil || currentCompatibilityRows != 35 {
		t.Fatalf("active artifacts with current compatibility schema = %d, error=%v", currentCompatibilityRows, err)
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
	var overlayArtifacts int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT count(*) FROM core_artifacts
WHERE enabled=1 AND emulatorjs_version='4.3.0-pre'
AND core_id IN ('dosbox_pure','genesis_plus_gx_wide','azahar')
`).Scan(&overlayArtifacts); err != nil || overlayArtifacts != 3 {
		t.Fatalf("4.3 overlay artifacts = %d, error=%v", overlayArtifacts, err)
	}
	var baseArtifacts int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT count(*) FROM core_artifacts
WHERE enabled=1 AND emulatorjs_version='4.2.3'
AND core_id IN ('beetle_vb','mednafen_wswan','smsplus','fbalpha2012_cps1','fbalpha2012_cps2')
`).Scan(&baseArtifacts); err != nil || baseArtifacts != 5 {
		t.Fatalf("4.2 expansion artifacts = %d, error=%v", baseArtifacts, err)
	}

	var pendingDATs, activeDATs int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT sum(CASE WHEN parse_status='PENDING' THEN 1 ELSE 0 END),
sum(is_active)
FROM dat_versions
`).Scan(&pendingDATs, &activeDATs); err != nil {
		t.Fatalf("count pending DATs: %v", err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return pendingDATs != 5 }, func() bool { return activeDATs != 0 }), "pre-index DATs pending/active = %d/%d, want 5/0", pendingDATs, activeDATs)
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
	testassert.Falsef(t, testassert.Any(func() bool { return staticBIOS != 22 }, func() bool { return matchedCatalog != staticBIOS }), "static BIOS catalog = %d/%d, want 22/22", staticBIOS, matchedCatalog)
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
`).Scan(&compatibility); err != nil || !strings.Contains(compatibility, `"requestedArtifactBasename":"ppsspp-thread-wasm.data"`) {
		t.Fatalf("PPSSPP compatibility = %s, error=%v", compatibility, err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return !strings.Contains(compatibility, `"schemaVersion":5`) }, func() bool { return !strings.Contains(compatibility, `"supportedContentKinds":["SINGLE_FILE"]`) }, func() bool { return strings.Contains(compatibility, `"MULTI_DISC_M3U_V1"`) }), "PPSSPP V5 capability = %s", compatibility)
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT compatibility_config_json FROM core_artifacts
WHERE core_id='beetle_vb' AND enabled=1
`).Scan(&compatibility); err != nil || !strings.Contains(compatibility, `"delayMs":25000`) ||
		strings.Count(compatibility, `"kind":"PRESS_CONTROL"`) != 4 {
		t.Fatalf("Beetle VB startup actions = %s, error=%v", compatibility, err)
	}
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT compatibility_config_json FROM core_artifacts
WHERE core_id='azahar' AND enabled=1
`).Scan(&compatibility); err != nil || !strings.Contains(compatibility, `"inputMode":"POINTER"`) ||
		!strings.Contains(compatibility, `"webgl2Enabled":"enabled"`) {
		t.Fatalf("Azahar compatibility = %s, error=%v", compatibility, err)
	}
	var yabauseID string
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT id,compatibility_config_json
FROM core_artifacts
WHERE core_id='yabause' AND enabled=1
`).Scan(&yabauseID, &compatibility); err != nil ||
		!strings.Contains(compatibility, `"supportedContentKinds":["SINGLE_FILE","MULTI_DISC_M3U_V1"]`) ||
		!strings.Contains(compatibility, `"maxTotalBytes":1073741824`) {
		t.Fatalf("yabause compatibility = %s, error=%v", compatibility, err)
	}

	// Simulate a drifted current-lineage row and prove bootstrap restores the
	// manifest declaration exactly once without replacing artifact identity.
	if _, err := database.SQL.ExecContext(context.Background(), `
UPDATE core_artifacts
SET compatibility_config_json='{"schemaVersion":5}',version=7
WHERE id=?
`, yabauseID); err != nil {
		t.Fatal(err)
	}
	reconcileTime := bootstrapTime.Add(time.Second)
	if err := manifest.Bootstrap(context.Background(), database.SQL, reconcileTime); err != nil {
		t.Fatalf("reconcile bootstrap: %v", err)
	}
	var versionNumber, updatedAtMS int64
	var currentID string
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT id,version,updated_at_ms,compatibility_config_json
FROM core_artifacts
WHERE core_id='yabause' AND enabled=1
`).Scan(&currentID, &versionNumber, &updatedAtMS, &compatibility); err != nil || currentID != yabauseID ||
		versionNumber != 8 || updatedAtMS != reconcileTime.UnixMilli() || !strings.Contains(compatibility, `"schemaVersion":5`) {
		t.Fatalf("reconciled artifact = id:%s version:%d updated:%d compatibility:%s error:%v", currentID, versionNumber, updatedAtMS, compatibility, err)
	}
	if err := manifest.Bootstrap(context.Background(), database.SQL, reconcileTime.Add(time.Hour)); err != nil {
		t.Fatalf("repeat bootstrap: %v", err)
	}
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT version,updated_at_ms FROM core_artifacts WHERE id=?
`, yabauseID).Scan(&versionNumber, &updatedAtMS); err != nil || versionNumber != 8 || updatedAtMS != reconcileTime.UnixMilli() {
		t.Fatalf("idempotent artifact = version:%d updated:%d error:%v", versionNumber, updatedAtMS, err)
	}

	// Bootstrap is intentionally idempotent; every process start verifies the
	// same selected release without creating duplicate rows.
	if err := manifest.Bootstrap(context.Background(), database.SQL, reconcileTime.Add(2*time.Hour)); err != nil {
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
