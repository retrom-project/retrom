//go:build integration

package saves

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/runtimebundle"
	"retrom/internal/runtimecatalog"
	"retrom/internal/runtimeprovider"
	"retrom/internal/testsupport"
)

func TestCatalogExtensionPreservesInitializedGamesReviewsSettingsAndSaves(t *testing.T) {
	fixture := newSaveFixture(t)
	original := fixture.createLaunch(t)
	saved, _, err := fixture.saves.CreateManual(fixture.ctx, original.LaunchID, original.Capability,
		uuid.NewString(), manualRequest(t, "Persistent checkpoint", []byte("unchanged state"), nil))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fixture.createValidationLaunch(t)
	mustSaveSQL(t, fixture.database.SQL, `UPDATE platform_instances SET name='My configured folder',sort_order=321,version=version+1 WHERE catalog_template_key='gba/mgba'`)
	before := catalogUserEvidence(t, fixture.database.SQL)
	active, manifests, err := testsupport.RuntimeProviderInputs(fixture.ctx, fixture.database.SQL)
	if err != nil {
		t.Fatal(err)
	}
	catalog := extensionDeclarations(t)
	extendFixtureProvider(t, &active, manifests)
	projection, err := runtimeprovider.NewProjection(active, manifests, catalog)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := runtimeprovider.Reconcile(fixture.ctx, fixture.database.SQL, projection, *fixture.now); err != nil {
			t.Fatal(err)
		}
	}
	if after := catalogUserEvidence(t, fixture.database.SQL); after != before {
		t.Fatal("extension changed schema or user-owned records")
	}
	if violations := queryEvidence(t, fixture.database.SQL, "PRAGMA foreign_key_check"); len(violations) != 0 {
		t.Fatalf("extension broke references: %#v", violations)
	}
	var count int
	if err := fixture.database.SQL.QueryRowContext(fixture.ctx, `SELECT count(*) FROM runtime_target_bindings WHERE core_id='extension-core' AND target_id='extension-target'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("extension binding count=%d err=%v", count, err)
	}
	restored := fixture.createLaunchFromSave(t, &saved.SaveStateID)
	digest, err := fixture.saves.StateDigest(fixture.ctx, restored.LaunchID, restored.Capability)
	if err != nil || digest != fmt.Sprintf("%x", sha256.Sum256([]byte("unchanged state"))) {
		t.Fatalf("existing save no longer restores exact payload: %s %v", digest, err)
	}
}

func extensionDeclarations(t *testing.T) runtimecatalog.Catalog {
	t.Helper()
	contents, err := os.ReadFile("../../data/runtime-target-bindings/v1/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := runtimecatalog.ParseCatalog(contents)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Definitions.Cores = append(catalog.Definitions.Cores, runtimecatalog.CoreDefinition{ID: "extension-core", Name: "Mechanism fixture", Enabled: true})
	catalog.Definitions.AssetPacks = append(catalog.Definitions.AssetPacks, runtimecatalog.AssetPackDefinition{
		ID: "extension-assets", Kind: "EXTRA_ASSETS", Generation: "RPGXP", DeclaredName: "Extra Assets",
		NormalizedDeclaredName: "extra assets", DisplayName: "Extra assets", RequiredLayoutVersion: "mkxpz-v1", Enabled: true,
	})
	catalog.Bindings = append(catalog.Bindings, runtimecatalog.Binding{
		ID: "extension-core", CoreID: "extension-core", ProviderID: "emulatorjs", TargetID: "extension-target",
		PlatformIDs: []string{"gba"}, AcceptedContentKinds: []string{"SINGLE_FILE"},
		DetectorProfile: "EMULATORJS_SINGLE_FILE", LaunchPolicy: "SUPPORTED",
	})
	slices.SortFunc(catalog.Definitions.Cores, func(a, b runtimecatalog.CoreDefinition) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(catalog.Definitions.AssetPacks, func(a, b runtimecatalog.AssetPackDefinition) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(catalog.Bindings, func(a, b runtimecatalog.Binding) int {
		return strings.Compare(a.ProviderID+"/"+a.TargetID, b.ProviderID+"/"+b.TargetID)
	})
	return catalog
}

func extendFixtureProvider(t *testing.T, active *runtimebundle.ActiveDescriptor, manifests map[string]runtimebundle.Manifest) {
	t.Helper()
	manifest := manifests["emulatorjs"]
	manifest.ProviderVersion = "1.1.0"
	index := slices.IndexFunc(manifest.Targets, func(target runtimebundle.Target) bool { return target.ID == "mgba" })
	if index < 0 {
		t.Fatal("missing fixture source target")
	}
	extra := manifest.Targets[index]
	extra.ID = "extension-target"
	manifest.Targets = append(manifest.Targets, extra)
	slices.SortFunc(manifest.Targets, func(a, b runtimebundle.Target) int { return strings.Compare(a.ID, b.ID) })
	manifests["emulatorjs"] = manifest
	for index := range active.Providers {
		provider := &active.Providers[index]
		if provider.ProviderID != "emulatorjs" {
			continue
		}
		provider.ProviderVersion = manifest.ProviderVersion
		provider.BundleSHA256, provider.ManifestSHA256, provider.ModuleSHA256 = strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
		provider.Targets = append(provider.Targets, runtimebundle.ActiveTarget{ID: extra.ID, Checkpoint: extra.Checkpoint})
		slices.SortFunc(provider.Targets, func(a, b runtimebundle.ActiveTarget) int { return strings.Compare(a.ID, b.ID) })
	}
}

func catalogUserEvidence(t *testing.T, database *sql.DB) [32]byte {
	t.Helper()
	queries := []string{
		`SELECT sql FROM sqlite_schema WHERE sql IS NOT NULL ORDER BY name`,
		`SELECT * FROM schema_migrations ORDER BY version`,
		`SELECT * FROM games ORDER BY id`, `SELECT * FROM game_files ORDER BY rowid`,
		`SELECT * FROM game_variants ORDER BY id`, `SELECT * FROM save_states ORDER BY id`,
		`SELECT * FROM import_items ORDER BY id`, `SELECT * FROM review_drafts ORDER BY id`,
		`SELECT * FROM rpgmaker_review_profiles ORDER BY rowid`,
		`SELECT * FROM platform_instances ORDER BY id`, `SELECT * FROM profiles ORDER BY id`,
	}
	var evidence [][]any
	for _, query := range queries {
		evidence = append(evidence, queryEvidence(t, database, query)...)
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(encoded)
}

func queryEvidence(t *testing.T, database *sql.DB, query string) [][]any {
	t.Helper()
	rows, err := database.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var result [][]any
	for rows.Next() {
		values, pointers := make([]any, len(columns)), make([]any, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		result = append(result, values)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}
