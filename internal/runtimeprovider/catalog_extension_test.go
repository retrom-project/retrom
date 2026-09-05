package runtimeprovider

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"retrom/internal/runtimebundle"
	"retrom/internal/runtimecatalog"
)

func TestDeclaredCoreCanBeAddedToInitializedDatabaseWithoutSchemaChange(t *testing.T) {
	database := openProjectionDatabase(t)
	initial := projectionFixture("1.0.0", "a", []string{"state-v1"})
	if err := Reconcile(t.Context(), database.SQL, initial, time.UnixMilli(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(t.Context(), `
INSERT INTO platform_instances(id,platform_id,default_core_id,name,slug,description,
sort_order,enabled,version,created_at_ms,updated_at_ms)
VALUES('custom','gbc','gambatte','My custom folder','custom','Keep my settings',42,0,1,1,1);
`); err != nil {
		t.Fatal(err)
	}
	var schemaBefore string
	if err := database.SQL.QueryRowContext(t.Context(), `SELECT group_concat(sql,';') FROM sqlite_schema WHERE sql IS NOT NULL`).Scan(&schemaBefore); err != nil {
		t.Fatal(err)
	}
	// The extension is a declaration using existing ROM delivery, not a SQL seed.
	contents := []byte(`{
"schemaVersion":1,
"definitions":{
 "platforms":[{"id":"gbc","name":"Game Boy / Color","sortOrder":40,"enabled":true}],
 "cores":[{"id":"gambatte","name":"Gambatte","enabled":true},{"id":"new-core","name":"New Core","enabled":true}],
 "contentKinds":["SINGLE_FILE"],"assetPacks":[]
},
"bindings":[{"id":"fixture-extra","coreId":"new-core","providerId":"fixture","targetId":"extra",
"platformIds":["gbc"],"acceptedContentKinds":["SINGLE_FILE"],"detectorProfile":"EMULATORJS_SINGLE_FILE",
"deliveryProfile":"EMULATORJS_CONTENT","launchPolicy":"SUPPORTED","reviewPolicy":"NONE"},
{"id":"fixture-target","coreId":"gambatte","providerId":"fixture","targetId":"target",
"platformIds":["gbc"],"acceptedContentKinds":["SINGLE_FILE"],"detectorProfile":"EMULATORJS_SINGLE_FILE",
"deliveryProfile":"EMULATORJS_CONTENT","launchPolicy":"SUPPORTED","reviewPolicy":"NONE"}]
}`)
	catalog, err := runtimecatalog.ParseCatalog(contents)
	if err != nil {
		t.Fatal(err)
	}
	// Roundtrip through the public catalog boundary; never fabricate projection internals.
	encoded, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "New Core") {
		t.Fatal("product definitions were discarded")
	}
	if err := reconcileCatalogExtension(t, database.SQL, initial, catalog); err != nil {
		t.Fatal(err)
	}
	assertExtensionPreservesFolder(t, database.SQL, schemaBefore)
}

func assertExtensionPreservesFolder(t *testing.T, database *sql.DB, schemaBefore string) {
	t.Helper()
	var schemaAfter, folderName, coreID string
	var enabled, order, schemaVersion int
	if err := database.QueryRowContext(t.Context(), `SELECT group_concat(sql,';') FROM sqlite_schema WHERE sql IS NOT NULL`).Scan(&schemaAfter); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(t.Context(), `SELECT name,default_core_id,enabled,sort_order FROM platform_instances WHERE id='custom'`).Scan(&folderName, &coreID, &enabled, &order); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(t.Context(), `SELECT max(version) FROM schema_migrations`).Scan(&schemaVersion); err != nil {
		t.Fatal(err)
	}
	if schemaAfter != schemaBefore || schemaVersion != 10 || folderName != "My custom folder" || coreID != "gambatte" || enabled != 0 || order != 42 {
		t.Fatal("catalog update changed schema or user configuration")
	}
}

func reconcileCatalogExtension(t *testing.T, database *sql.DB, initial Projection, catalog runtimecatalog.Catalog) error {
	t.Helper()
	provider := initial.providers[0].active
	provider.ProviderVersion = "1.1.0"
	provider.BundleSHA256 = strings.Repeat("b", 64)
	provider.ManifestSHA256 = provider.BundleSHA256
	provider.ModuleSHA256 = provider.BundleSHA256
	provider.InstallationPath = "fixture/" + provider.BundleSHA256
	target := initial.providers[0].targets[0].target
	extra := target
	extra.ID = "extra"
	provider.Targets = append(provider.Targets, runtimebundle.ActiveTarget{ID: extra.ID, Checkpoint: extra.Checkpoint})
	active := runtimebundle.ActiveDescriptor{
		SchemaVersion: 1, Source: "candidate", SourceTreeSHA256: &provider.BundleSHA256,
		Providers: []runtimebundle.ActiveProvider{provider},
	}
	candidate, err := NewProjection(active, map[string]runtimebundle.Manifest{"fixture": {
		SchemaVersion: 1, ProviderID: "fixture", ProviderVersion: provider.ProviderVersion, ProviderAPI: 1,
		ClientModulePath: "client.mjs", Targets: []runtimebundle.Target{extra, target},
	}}, catalog)
	if err != nil {
		return err
	}
	if err := Reconcile(t.Context(), database, candidate, time.UnixMilli(2)); err != nil {
		return err
	}
	return Reconcile(t.Context(), database, candidate, time.UnixMilli(3))
}

func TestDeclaredCoreRemovalCannotOrphanUserConfiguration(t *testing.T) {
	database := openProjectionDatabase(t)
	initial := projectionFixture("1.0.0", "a", []string{"state-v1"})
	if err := Reconcile(t.Context(), database.SQL, initial, time.UnixMilli(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(t.Context(), `INSERT INTO platform_instances(id,platform_id,default_core_id,name,slug,sort_order,enabled,version,created_at_ms,updated_at_ms) VALUES('custom','gbc','gambatte','My folder','custom',1,1,1,1,1)`); err != nil {
		t.Fatal(err)
	}
	candidate := projectionFixture("1.1.0", "b", []string{"state-v1"})
	candidate.definitions.Cores[0].ID = "replacement"
	candidate.bindings[0].CoreID = "replacement"
	candidate.catalogSHA256 = strings.Repeat("c", 64)
	if err := Reconcile(t.Context(), database.SQL, candidate, time.UnixMilli(2)); err == nil {
		t.Fatal("omitting a referenced core was silently accepted")
	}
	var version, core string
	if err := database.SQL.QueryRowContext(t.Context(), `SELECT (SELECT provider_version FROM runtime_providers WHERE provider_id='fixture'),default_core_id FROM platform_instances WHERE id='custom'`).Scan(&version, &core); err != nil {
		t.Fatal(err)
	}
	if version != "1.0.0" || core != "gambatte" {
		t.Fatal("failed sync partially changed runtime or user configuration")
	}
}

func TestUnusedProductDefinitionCanBeRemovedWithoutSchemaChange(t *testing.T) {
	database := openProjectionDatabase(t)
	initial := projectionFixture("1.0.0", "a", []string{"state-v1"})
	initial.definitions.Cores = append([]runtimecatalog.CoreDefinition{{ID: "dormant", Name: "Unused", Enabled: true}}, initial.definitions.Cores...)
	initial.catalogSHA256 = strings.Repeat("c", 64)
	if err := Reconcile(t.Context(), database.SQL, initial, time.UnixMilli(1)); err != nil {
		t.Fatal(err)
	}
	current := projectionFixture("1.0.0", "a", []string{"state-v1"})
	if err := Reconcile(t.Context(), database.SQL, current, time.UnixMilli(2)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.SQL.QueryRowContext(t.Context(), `SELECT count(*) FROM cores WHERE id='dormant'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("unused omitted definition remained active")
	}
}
