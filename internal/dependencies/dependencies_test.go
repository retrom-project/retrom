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

	var selectedArtifacts int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT count(*)
FROM core_artifacts
WHERE selected_for_new_bindings=1 AND available_for_launch=1
`).Scan(&selectedArtifacts); err != nil {
		t.Fatalf("count selected artifacts: %v", err)
	}
	selectedRPGMakerArtifacts := 0
	for _, artifact := range manifest.RPGMaker.Manifest.Artifacts {
		if artifact.SelectedForNewBindings {
			selectedRPGMakerArtifacts++
		}
	}
	expectedSelected := 35 + selectedRPGMakerArtifacts
	testassert.Falsef(t, selectedArtifacts != expectedSelected,
		"selected artifacts = %d, want %d", selectedArtifacts, expectedSelected)
	var onsFamily, onsAdapter, onsVersion, onsPayload string
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT runtime_family,runtime_adapter_kind,runtime_version,save_payload_kind
FROM core_artifacts
WHERE core_id='onscripter_yuri' AND route_key='ONS_YURI'
AND selected_for_new_bindings=1 AND available_for_launch=1
`).Scan(&onsFamily, &onsAdapter, &onsVersion, &onsPayload); err != nil ||
		onsFamily != "ONS" || onsAdapter != "ONS_YURI_WEB" || onsVersion != "v0.7.6" ||
		onsPayload != "ONS_SAVE_BUNDLE_V1" {
		t.Fatalf("ONS artifact = %q/%q/%q/%q, error=%v", onsFamily, onsAdapter, onsVersion, onsPayload, err)
	}
	var currentCompatibilityRows int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT count(*)
FROM core_artifacts
WHERE selected_for_new_bindings=1
AND json_extract(compatibility_json,'$.schemaVersion')=5
AND json_extract(compatibility_json,'$.adapterAbi')='emulatorjs-state-v1'
`).Scan(&currentCompatibilityRows); err != nil || currentCompatibilityRows != 35 {
		t.Fatalf("selected artifacts with current compatibility schema = %d, error=%v", currentCompatibilityRows, err)
	}
	var dosVersion string
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT runtime_version
FROM core_artifacts
WHERE core_id='dosbox_pure' AND selected_for_new_bindings=1
`).Scan(&dosVersion); err != nil || dosVersion != "4.3.0-pre" {
		t.Fatalf("selected DOS artifact = %q, error=%v", dosVersion, err)
	}
	var oldDOSSelected, oldDOSAvailable int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT sum(selected_for_new_bindings),sum(available_for_launch)
FROM core_artifacts
WHERE core_id='dosbox_pure' AND runtime_version='4.2.3'
`).Scan(&oldDOSSelected, &oldDOSAvailable); err != nil || oldDOSSelected != 0 || oldDOSAvailable != 1 {
		t.Fatalf("old DOS artifact selected/available = %d/%d, error=%v", oldDOSSelected, oldDOSAvailable, err)
	}
	var overlayArtifacts int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT count(*) FROM core_artifacts
WHERE selected_for_new_bindings=1 AND runtime_version='4.3.0-pre'
AND core_id IN ('dosbox_pure','genesis_plus_gx_wide','azahar')
`).Scan(&overlayArtifacts); err != nil || overlayArtifacts != 3 {
		t.Fatalf("4.3 overlay artifacts = %d, error=%v", overlayArtifacts, err)
	}
	var baseArtifacts int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT count(*) FROM core_artifacts
WHERE selected_for_new_bindings=1 AND runtime_version='4.2.3'
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
SELECT compatibility_json
FROM core_artifacts
WHERE core_id='ppsspp' AND selected_for_new_bindings=1
`).Scan(&compatibility); err != nil || !strings.Contains(compatibility, `"requestedArtifactBasename":"ppsspp-thread-wasm.data"`) {
		t.Fatalf("PPSSPP compatibility = %s, error=%v", compatibility, err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return !strings.Contains(compatibility, `"schemaVersion":5`) }, func() bool { return !strings.Contains(compatibility, `"supportedContentKinds":["SINGLE_FILE"]`) }, func() bool { return strings.Contains(compatibility, `"MULTI_DISC_M3U_V1"`) }), "PPSSPP V5 capability = %s", compatibility)
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT compatibility_json FROM core_artifacts
WHERE core_id='beetle_vb' AND selected_for_new_bindings=1
`).Scan(&compatibility); err != nil || !strings.Contains(compatibility, `"delayMs":25000`) ||
		strings.Count(compatibility, `"kind":"PRESS_CONTROL"`) != 4 {
		t.Fatalf("Beetle VB startup actions = %s, error=%v", compatibility, err)
	}
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT compatibility_json FROM core_artifacts
WHERE core_id='azahar' AND selected_for_new_bindings=1
`).Scan(&compatibility); err != nil || !strings.Contains(compatibility, `"inputMode":"POINTER"`) ||
		!strings.Contains(compatibility, `"webgl2Enabled":"enabled"`) {
		t.Fatalf("Azahar compatibility = %s, error=%v", compatibility, err)
	}
	var yabauseID string
	var versionNumber, updatedAtMS int64
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT id,version,updated_at_ms,compatibility_json
FROM core_artifacts
WHERE core_id='yabause' AND selected_for_new_bindings=1
`).Scan(&yabauseID, &versionNumber, &updatedAtMS, &compatibility); err != nil ||
		!strings.Contains(compatibility, `"supportedContentKinds":["SINGLE_FILE","MULTI_DISC_M3U_V1"]`) ||
		!strings.Contains(compatibility, `"maxTotalBytes":1073741824`) {
		t.Fatalf("yabause compatibility = %s, error=%v", compatibility, err)
	}

	// Artifact payload identity is immutable. Bootstrap can only switch the
	// current selector, never rewrite compatibility bytes in place.
	if _, err := database.SQL.ExecContext(context.Background(), `
UPDATE core_artifacts
SET compatibility_json='{"schemaVersion":5}'
WHERE id=?
`, yabauseID); err == nil {
		t.Fatal("immutable artifact compatibility update succeeded")
	}
	reconcileTime := bootstrapTime.Add(time.Second)
	if err := manifest.Bootstrap(context.Background(), database.SQL, reconcileTime); err != nil {
		t.Fatalf("idempotent bootstrap: %v", err)
	}
	var currentID string
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT id,version,updated_at_ms,compatibility_json
FROM core_artifacts
WHERE core_id='yabause' AND selected_for_new_bindings=1
`).Scan(&currentID, &versionNumber, &updatedAtMS, &compatibility); err != nil || currentID != yabauseID ||
		versionNumber != 1 || updatedAtMS != bootstrapTime.UnixMilli() || !strings.Contains(compatibility, `"schemaVersion":5`) {
		t.Fatalf("idempotent artifact = id:%s version:%d updated:%d compatibility:%s error:%v", currentID, versionNumber, updatedAtMS, compatibility, err)
	}
	if err := manifest.Bootstrap(context.Background(), database.SQL, reconcileTime.Add(time.Hour)); err != nil {
		t.Fatalf("repeat bootstrap: %v", err)
	}
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT version,updated_at_ms FROM core_artifacts WHERE id=?
	`, yabauseID).Scan(&versionNumber, &updatedAtMS); err != nil || versionNumber != 1 || updatedAtMS != bootstrapTime.UnixMilli() {
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

func TestRPGMakerBootstrapReusesUnchangedArtifactAcrossManifestRevision(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	testassert.True(t, ok, "locate test file")
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	set, err := Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3", "4.3.0-pre"}, "4.2.3")
	testassert.Falsef(t, err != nil, "load dependencies: %v", err)
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	testassert.Falsef(t, err != nil, "open database: %v", err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	bootstrapTime := time.Now().Truncate(time.Millisecond)
	if err := set.Bootstrap(context.Background(), database.SQL, bootstrapTime); err != nil {
		t.Fatalf("bootstrap dependencies: %v", err)
	}
	var originalID, originalManifest string
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT id,manifest_sha256 FROM core_artifacts
WHERE core_id='rpgmaker_2000' AND selected_for_new_bindings=1
`).Scan(&originalID, &originalManifest); err != nil {
		t.Fatal(err)
	}
	set.RPGMaker.ManifestSHA256 = strings.Repeat("f", 64)
	if err := set.Bootstrap(context.Background(), database.SQL, bootstrapTime.Add(time.Second)); err != nil {
		t.Fatalf("bootstrap revised RPG Maker manifest: %v", err)
	}
	var currentID, currentManifest string
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT id,manifest_sha256 FROM core_artifacts
WHERE core_id='rpgmaker_2000' AND selected_for_new_bindings=1
`).Scan(&currentID, &currentManifest); err != nil {
		t.Fatal(err)
	}
	if currentID != originalID || currentManifest != originalManifest {
		t.Fatalf("reused artifact = %s/%s, want %s/%s", currentID, currentManifest, originalID, originalManifest)
	}
}

func TestRPGMakerBootstrapDeselectsUndeclaredArtifact(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	testassert.True(t, ok, "locate test file")
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	set, err := Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3", "4.3.0-pre"}, "4.2.3")
	testassert.Falsef(t, err != nil, "load dependencies: %v", err)
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	testassert.Falsef(t, err != nil, "open database: %v", err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })

	bootstrapTime := time.Now().Truncate(time.Millisecond)
	if err := set.Bootstrap(context.Background(), database.SQL, bootstrapTime); err != nil {
		t.Fatalf("bootstrap dependencies: %v", err)
	}
	if _, err := database.SQL.ExecContext(context.Background(), `
UPDATE core_artifacts SET selected_for_new_bindings=0
WHERE core_id='rpgmaker_2000' AND selected_for_new_bindings=1;
INSERT INTO core_artifacts(
 id,core_id,route_key,runtime_family,runtime_adapter_kind,runtime_version,adapter_id,
 entry_path,size_bytes,sha256,manifest_sha256,artifact_set_sha256,requires_threads,
 save_payload_kind,save_max_bytes,provenance_json,compatibility_json,
 selected_for_new_bindings,available_for_launch,version,created_at_ms,updated_at_ms)
SELECT '01980000-0000-7000-8000-000000000099',core_id,'RPG2000_UNDECLARED',runtime_family,
 runtime_adapter_kind,runtime_version,adapter_id,entry_path,size_bytes,sha256,manifest_sha256,
 ?,requires_threads,save_payload_kind,save_max_bytes,provenance_json,compatibility_json,
 1,1,1,created_at_ms,updated_at_ms
FROM core_artifacts
WHERE core_id='rpgmaker_2000' AND route_key='RPG2000_EASYRPG'
`, strings.Repeat("f", 64)); err != nil {
		t.Fatalf("seed stale artifact: %v", err)
	}

	if err := set.Bootstrap(context.Background(), database.SQL, bootstrapTime.Add(time.Second)); err != nil {
		t.Fatalf("bootstrap with stale prior route: %v", err)
	}
	var selectedRoute string
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT route_key FROM core_artifacts
WHERE core_id='rpgmaker_2000' AND selected_for_new_bindings=1
`).Scan(&selectedRoute); err != nil {
		t.Fatal(err)
	}
	if selectedRoute != "RPG2000_EASYRPG" {
		t.Fatalf("selected route = %s, want RPG2000_EASYRPG", selectedRoute)
	}
	var staleSelected, staleAvailable int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT selected_for_new_bindings,available_for_launch FROM core_artifacts
WHERE id='01980000-0000-7000-8000-000000000099'
`).Scan(&staleSelected, &staleAvailable); err != nil {
		t.Fatal(err)
	}
	if staleSelected != 0 || staleAvailable != 0 {
		t.Fatalf("stale artifact selected/available = %d/%d, want 0/0", staleSelected, staleAvailable)
	}
}
