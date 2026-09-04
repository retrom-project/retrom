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
	candidate := projectionFixture("1.0.0", "a", 1, []string{"state-v1"})

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
}

func assertProjectionCounts(t *testing.T, database *sql.DB) {
	t.Helper()
	var targetCount, catalogVersion, auditCount int
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
	if err := database.QueryRowContext(t.Context(), `SELECT catalog_version FROM runtime_catalog_state WHERE singleton=1`).Scan(&catalogVersion); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(t.Context(), `
SELECT count(*) FROM audit_events WHERE action='RUNTIME_PROVIDER_RECONCILED'
`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if targetCount != 1 || bindingCount != 1 || platformCount != 1 || contentKindCount != 1 || catalogVersion != 1 || auditCount != 1 {
		t.Fatalf("projection target/binding/platform/content/catalog/audit = %d/%d/%d/%d/%d/%d",
			targetCount, bindingCount, platformCount, contentKindCount, catalogVersion, auditCount)
	}
}

func TestReconcileRejectsNonForwardProviderAndCatalogChanges(t *testing.T) {
	sameCatalogRebuilt := projectionFixture("1.1.0", "b", 2, []string{"state-v1"})
	sameCatalogRebuilt.catalogSHA256 = strings.Repeat("c", 64)
	for _, test := range []struct {
		name      string
		candidate Projection
		expected  error
	}{
		{"provider downgrade", projectionFixture("0.9.0", "b", 2, []string{"state-v1"}), ErrProviderDowngrade},
		{"same provider version rebuilt", projectionFixture("1.0.0", "b", 3, []string{"state-v1"}), ErrProviderVersionRebuilt},
		{"catalog downgrade", projectionFixture("1.1.0", "b", 1, []string{"state-v1"}), ErrCatalogDowngrade},
		{"same catalog version rebuilt", sameCatalogRebuilt, ErrCatalogVersionRebuilt},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := openProjectionDatabase(t)
			if err := Reconcile(t.Context(), database.SQL, projectionFixture("1.0.0", "a", 2, []string{"state-v1"}), time.UnixMilli(1)); err != nil {
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
	if err := Reconcile(t.Context(), database.SQL, projectionFixture("1.0.0", "a", 1, []string{"state-v1"}), time.UnixMilli(1)); err != nil {
		t.Fatal(err)
	}
	upgrade := projectionFixture("1.1.0", "b", 2, []string{"state-v1", "state-v2"})
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

func TestReconcileRejectsReferencedTargetRemoval(t *testing.T) {
	database := openProjectionDatabase(t)
	initial := projectionFixtureForTarget("target", "1.0.0", "a", 1, []string{"state-v1"})
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
	candidate := projectionFixtureForTarget("replacement", "1.1.0", "b", 2, []string{"state-v1"})
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
CREATE TABLE rpgmaker_runtime_validations(id TEXT,provider_id TEXT,target_id TEXT);
CREATE TABLE rpgmaker_runtime_validation_checkpoints(validation_id TEXT,checkpoint_format TEXT);
INSERT INTO game_variants(game_id,provider_id,target_id) VALUES('game','fixture','target');
INSERT INTO save_states(game_id,checkpoint_format) VALUES('game','state-v1');
`); err != nil {
		t.Fatal(err)
	}
	transaction, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	upgrade := projectionFixture("1.1.0", "b", 2, []string{"state-v2"})
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

func projectionFixture(version, digestByte string, catalogVersion int, readFormats []string) Projection {
	return projectionFixtureForTarget("target", version, digestByte, catalogVersion, readFormats)
}

func projectionFixtureForTarget(targetID, version, digestByte string, catalogVersion int, readFormats []string) Projection {
	digest := strings.Repeat(digestByte, 64)
	checkpoint := &runtimebundle.Checkpoint{WriteFormat: readFormats[len(readFormats)-1], ReadFormats: readFormats, MaxBytes: 1024}
	target := runtimebundle.Target{
		ID: targetID, DisplayName: "Fixture",
		TargetOptionsSchema: runtimebundle.TargetOptionsSchema{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{}, "required": []any{},
		},
		Inputs:       []runtimebundle.Input{{Role: "game", Kind: "ROM_BLOB", Cardinality: "ONE"}},
		Capabilities: runtimebundle.Capabilities{Checkpoint: true, FrameMode: "NONE", VideoModes: []string{}, ValidationProbes: []string{}},
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
	catalog := runtimecatalog.Catalog{SchemaVersion: 1, CatalogVersion: catalogVersion, Bindings: []runtimecatalog.Binding{{
		ID: "fixture-" + targetID, CoreID: "gambatte", ProviderID: "fixture", TargetID: targetID,
		PlatformIDs: []string{"gbc"}, AcceptedContentKinds: []string{"SINGLE_FILE"},
		DetectorProfile: "ROM_FILE", DeliveryProfile: "ROM_BLOB", LaunchPolicy: "SUPPORTED", ReviewPolicy: "NONE",
	}}}
	projection, err := NewProjection(active, map[string]runtimebundle.Manifest{"fixture": {
		SchemaVersion: 1, ProviderID: "fixture", ProviderVersion: version, ProviderAPI: 1,
		ClientModulePath: "client.mjs", Targets: []runtimebundle.Target{target},
	}}, catalog)
	if err != nil {
		panic(err)
	}
	return projection
}
