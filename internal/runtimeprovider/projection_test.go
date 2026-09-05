package runtimeprovider

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/runtimebundle"
	"retrom/internal/runtimecatalog"
	"retrom/internal/store"
)

func TestReconcileProjectsProviderTargetsAndCatalogAtomically(t *testing.T) {
	database := openProjectionDatabase(t)
	candidate := projectionFixture("1.0.0", "a", []string{"state-v1"})

	if err := Reconcile(t.Context(), database.SQL, candidate, time.UnixMilli(1234)); err != nil {
		t.Fatal(err)
	}
	if err := Reconcile(t.Context(), database.SQL, candidate, time.UnixMilli(5678)); err != nil {
		t.Fatalf("idempotent reconcile: %v", err)
	}

	var providerVersion, bundleDigest string
	if err := database.SQL.QueryRowContext(t.Context(), `
SELECT provider_version,bundle_sha256 FROM runtime_providers WHERE provider_id='fixture'
`).Scan(&providerVersion, &bundleDigest); err != nil {
		t.Fatal(err)
	}
	if providerVersion != "1.0.0" || bundleDigest != strings.Repeat("a", 64) {
		t.Fatalf("provider projection = %q %q", providerVersion, bundleDigest)
	}
	assertProjectionCounts(t, database.SQL)
	var delivery, launch string
	if err := database.SQL.QueryRowContext(t.Context(), `
SELECT delivery_profile,launch_policy FROM runtime_target_bindings WHERE binding_id='fixture-target'
`).Scan(&delivery, &launch); err != nil {
		t.Fatal(err)
	}
	if delivery != "EMULATORJS_CONTENT" || launch != "SUPPORTED" {
		t.Fatalf("strategy-derived binding projection = %s/%s", delivery, launch)
	}
}

func assertProjectionCounts(t *testing.T, database *sql.DB) {
	t.Helper()
	var targetCount, catalogCount, auditCount int
	if err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM runtime_targets`).Scan(&targetCount); err != nil {
		t.Fatal(err)
	}
	var bindingCount, platformCount, contentKindCount int
	if err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM runtime_target_bindings`).Scan(&bindingCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM runtime_binding_platforms`).Scan(&platformCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM runtime_binding_content_kinds`).Scan(&contentKindCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM runtime_catalog_state WHERE singleton=1`).Scan(&catalogCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(t.Context(), `
SELECT count(*) FROM audit_events WHERE action='RUNTIME_PROVIDER_RECONCILED'
`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if targetCount != 1 || bindingCount != 1 || platformCount != 1 || contentKindCount != 1 || catalogCount != 1 || auditCount != 1 {
		t.Fatalf("projection target/binding/platform/content/catalog/audit = %d/%d/%d/%d/%d/%d",
			targetCount, bindingCount, platformCount, contentKindCount, catalogCount, auditCount)
	}
}

func TestReconcileRejectsNonForwardProviderChanges(t *testing.T) {
	for _, test := range []struct {
		name      string
		candidate Projection
		expected  error
	}{
		{"provider downgrade", projectionFixture("0.9.0", "b", []string{"state-v1"}), ErrProviderDowngrade},
		{"same provider version rebuilt", projectionFixture("1.0.0", "b", []string{"state-v1"}), ErrProviderVersionRebuilt},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := openProjectionDatabase(t)
			if err := Reconcile(t.Context(), database.SQL, projectionFixture("1.0.0", "a", []string{"state-v1"}), time.UnixMilli(1)); err != nil {
				t.Fatal(err)
			}
			if err := Reconcile(t.Context(), database.SQL, test.candidate, time.UnixMilli(2)); !errors.Is(err, test.expected) {
				t.Fatalf("error = %v, want %v", err, test.expected)
			}
		})
	}
}

func TestReconcileAllowsOnlyForwardCompatibleProviderUpgrade(t *testing.T) {
	database := openProjectionDatabase(t)
	if err := Reconcile(t.Context(), database.SQL, projectionFixture("1.0.0", "a", []string{"state-v1"}), time.UnixMilli(1)); err != nil {
		t.Fatal(err)
	}
	upgrade := projectionFixture("1.1.0", "b", []string{"state-v1", "state-v2"})
	if err := Reconcile(t.Context(), database.SQL, upgrade, time.UnixMilli(2)); err != nil {
		t.Fatal(err)
	}
	var version, checkpointJSON string
	if err := database.SQL.QueryRowContext(t.Context(), `
SELECT provider.provider_version,target.checkpoint_json
FROM runtime_providers provider JOIN runtime_targets target ON target.provider_id=provider.provider_id
`).Scan(&version, &checkpointJSON); err != nil {
		t.Fatal(err)
	}
	if version != "1.1.0" || !strings.Contains(checkpointJSON, "state-v2") {
		t.Fatalf("upgraded projection = %q %q", version, checkpointJSON)
	}
}

func TestCatalogContentChangeNeedsNoIndependentVersionCounter(t *testing.T) {
	database := openProjectionDatabase(t)
	initial := projectionFixture("1.0.0", "a", []string{"state-v1"})
	if err := Reconcile(t.Context(), database.SQL, initial, time.UnixMilli(1)); err != nil {
		t.Fatal(err)
	}
	changed := initial
	changed.catalogSHA256 = strings.Repeat("b", 64)
	if err := Reconcile(t.Context(), database.SQL, changed, time.UnixMilli(2)); err != nil {
		t.Fatalf("current catalog change required an unrelated version counter: %v", err)
	}
}

func TestReconcileRejectsReferencedTargetRemoval(t *testing.T) {
	database := openProjectionDatabase(t)
	initial := projectionFixtureForTarget("target", "1.0.0", "a", []string{"state-v1"})
	if err := Reconcile(t.Context(), database.SQL, initial, time.UnixMilli(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(t.Context(), `
INSERT INTO bios_requirements(
  id,core_id,provider_id,target_id,source_kind,logical_name,
  requirement_mode,catalog_digest,source_url,source_version,enabled,version,created_at_ms,updated_at_ms
) VALUES('requirement','gambatte','fixture','target','STATIC','bios.bin','REQUIRED',?,
  'https://example.invalid/bios','1',1,1,1,1)
`, strings.Repeat("d", 64)); err != nil {
		t.Fatal(err)
	}
	candidate := projectionFixtureForTarget("replacement", "1.1.0", "b", []string{"state-v1"})
	if err := Reconcile(t.Context(), database.SQL, candidate, time.UnixMilli(2)); !errors.Is(err, ErrProviderTargetReferenced) {
		t.Fatalf("error = %v", err)
	}
}

func TestReconcileRejectsUnreadableStoredCheckpointFormat(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.ExecContext(t.Context(), `
CREATE TABLE save_states(game_id TEXT,checkpoint_format TEXT,deleted_at_ms INTEGER);
CREATE TABLE game_variants(game_id TEXT,provider_id TEXT,target_id TEXT);
INSERT INTO game_variants(game_id,provider_id,target_id) VALUES('game','fixture','target');
INSERT INTO save_states(game_id,checkpoint_format) VALUES('game','state-v1');
`); err != nil {
		t.Fatal(err)
	}
	transaction, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	upgrade := projectionFixture("1.1.0", "b", []string{"state-v2"})
	if err := validateCheckpointFormats(t.Context(), transaction, "fixture", upgrade.providers[0].targets[0]); !errors.Is(err, ErrProviderCheckpointUnreadable) {
		t.Fatalf("error = %v", err)
	}
}

func openProjectionDatabase(t *testing.T) *store.DB {
	t.Helper()
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func projectionFixture(version, digestByte string, readFormats []string) Projection {
	return projectionFixtureForTarget("target", version, digestByte, readFormats)
}

func projectionFixtureForTarget(targetID, version, digestByte string, readFormats []string) Projection {
	digest := strings.Repeat(digestByte, 64)
	checkpoint := &runtimebundle.Checkpoint{WriteFormat: readFormats[len(readFormats)-1], ReadFormats: readFormats, MaxBytes: 1024}
	target := runtimebundle.Target{
		ID: targetID, DisplayName: "Fixture",
		TargetOptionsSchema: runtimebundle.TargetOptionsSchema{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{
				"dosEntryPath":     map[string]any{"type": []any{"string", "null"}},
				"initialDiscIndex": map[string]any{"type": []any{"integer", "null"}, "minimum": 0},
			}, "required": []any{"dosEntryPath", "initialDiscIndex"},
		},
		Inputs:       []runtimebundle.Input{{Role: "game", Kind: "ROM_BLOB", Cardinality: "ONE"}},
		Capabilities: runtimebundle.Capabilities{Checkpoint: true, FrameMode: "NONE", VideoModes: []string{}},
		Checkpoint:   checkpoint, AssetPaths: []string{"client.mjs"},
	}
	active := runtimebundle.ActiveDescriptor{SchemaVersion: 1, Source: "candidate", SourceTreeSHA256: &digest, Providers: []runtimebundle.ActiveProvider{{
		ProviderID: "fixture", ProviderVersion: version, ProviderAPI: 1, BundleSHA256: digest,
		ManifestSHA256: digest, ModuleSHA256: digest, ClientModulePath: "client.mjs",
		InstallationPath: "fixture/" + digest, BundleSizeBytes: 1, FileCount: 3, UnpackedSizeBytes: 3,
		Targets: []runtimebundle.ActiveTarget{{
			ID: target.ID, Checkpoint: checkpoint,
		}},
	}}}
	catalog := runtimecatalog.Catalog{SchemaVersion: 1, Bindings: []runtimecatalog.Binding{{
		ID: "fixture-" + targetID, CoreID: "gambatte", ProviderID: "fixture", TargetID: targetID,
		PlatformIDs: []string{"gbc"}, AcceptedContentKinds: []string{"SINGLE_FILE"},
		DetectorProfile: "EMULATORJS_SINGLE_FILE", LaunchPolicy: "SUPPORTED",
	}}}
	catalog.Definitions = runtimecatalog.Definitions{
		Platforms:    []runtimecatalog.PlatformDefinition{{ID: "gbc", Name: "Game Boy / Color", SortOrder: 40, Enabled: true}},
		Cores:        []runtimecatalog.CoreDefinition{{ID: "gambatte", Name: "Gambatte", Enabled: true}},
		ContentKinds: []string{"SINGLE_FILE"}, AssetPacks: []runtimecatalog.AssetPackDefinition{},
	}
	projection, err := NewProjection(active, map[string]runtimebundle.Manifest{"fixture": {
		SchemaVersion: 1, ProviderID: "fixture", ProviderVersion: version, ProviderAPI: 1,
		ClientModulePath: "client.mjs", Targets: []runtimebundle.Target{target},
	}}, catalog)
	if err != nil {
		panic(err)
	}
	return projection
}

func TestProjectionRejectsOptionsOutsideRegisteredAccessStrategy(t *testing.T) {
	initial := projectionFixture("1.0.0", "a", []string{"state-v1"})
	provider := initial.providers[0].active
	target := initial.providers[0].targets[0].target
	target.TargetOptionsSchema = runtimebundle.TargetOptionsSchema{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{"unknownProperty": map[string]any{"type": "string"}}, "required": []any{"unknownProperty"},
	}
	_, err := NewProjection(runtimebundle.ActiveDescriptor{SchemaVersion: 1, Source: "candidate", Providers: []runtimebundle.ActiveProvider{provider}},
		map[string]runtimebundle.Manifest{"fixture": {SchemaVersion: 1, ProviderID: "fixture", ProviderVersion: "1.0.0", ProviderAPI: 1, ClientModulePath: "client.mjs", Targets: []runtimebundle.Target{target}}},
		runtimecatalog.Catalog{SchemaVersion: 1, Definitions: initial.definitions, Bindings: initial.bindings})
	if err == nil {
		t.Fatal("unsupported Host option access was accepted until launch time")
	}
}
