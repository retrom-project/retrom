package runtimeprovider

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"retrom/internal/testsupport/testpostgres"

	dbapi "retrom/internal/database"
	service "retrom/internal/service/runtimeprovider"

	runtimebundle "retrom/internal/runtime/bundle"
	runtimecatalog "retrom/internal/runtime/catalog"
)

func TestDeclaredCoreCanBeAddedToInitializedDatabaseWithoutSchemaChange(t *testing.T) {
	database := openProjectionDatabase(t)
	initial := projectionFixture("1.0.0", "a", []string{"state-v1"})
	if err := service.New(New(database.SQL)).Reconcile(t.Context(), initial, time.UnixMilli(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(t.Context(), `
INSERT INTO platform_instances(id,platform_id,default_core_id,name,slug,description,enabled,version,created_at_ms,updated_at_ms)
VALUES('custom','gbc','gambatte','My custom folder','custom','Keep my settings',0,1,1,1);
`); err != nil {
		t.Fatal(err)
	}
	var schemaBefore string
	if err := dbapi.QueryRowContext(t.Context(), database.SQL, `SELECT string_agg(sql,';' ORDER BY type,name) FROM (`+testpostgres.SchemaObjectsSQL+`) objects WHERE sql IS NOT NULL`).Scan(&schemaBefore); err != nil {
		t.Fatal(err)
	}
	// The extension is a declaration using existing ROM delivery, not a SQL seed.
	contents := []byte(`{
"schemaVersion":1,
"definitions":{
 "platforms":[{"id":"gbc","name":"Game Boy / Color","sortOrder":40,"enabled":true}],
 "cores":[{"id":"gambatte","name":"Gambatte","enabled":true},{"id":"new-core","name":"New Core","enabled":true}],
 "contentKinds":["SINGLE_FILE"]
},
"bindings":[{"id":"fixture-extra","coreId":"new-core","providerId":"fixture","targetId":"extra",
"platformIds":["gbc"],"acceptedContentKinds":["SINGLE_FILE"],"detectorProfile":"EMULATORJS_SINGLE_FILE",
"launchPolicy":"SUPPORTED"},
{"id":"fixture-target","coreId":"gambatte","providerId":"fixture","targetId":"target",
"platformIds":["gbc"],"acceptedContentKinds":["SINGLE_FILE"],"detectorProfile":"EMULATORJS_SINGLE_FILE",
"launchPolicy":"SUPPORTED"}]
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

func assertExtensionPreservesFolder(t *testing.T, database dbapi.DB, schemaBefore string) {
	t.Helper()
	var schemaAfter, folderName, coreID string
	var enabled, createdAt, schemaVersion int
	if err := dbapi.QueryRowContext(t.Context(), database, `SELECT string_agg(sql,';' ORDER BY type,name) FROM (`+testpostgres.SchemaObjectsSQL+`) objects WHERE sql IS NOT NULL`).Scan(&schemaAfter); err != nil {
		t.Fatal(err)
	}
	if err := dbapi.QueryRowContext(t.Context(), database, `SELECT name,default_core_id,enabled,created_at_ms FROM platform_instances WHERE id='custom'`).Scan(&folderName, &coreID, &enabled, &createdAt); err != nil {
		t.Fatal(err)
	}
	if err := dbapi.QueryRowContext(t.Context(), database, `SELECT max(version) FROM schema_migrations`).Scan(&schemaVersion); err != nil {
		t.Fatal(err)
	}
	if schemaAfter != schemaBefore || schemaVersion != 2 || folderName != "My custom folder" || coreID != "gambatte" || enabled != 0 || createdAt != 1 {
		t.Fatal("catalog update changed schema or user configuration")
	}
}

func reconcileCatalogExtension(t *testing.T, database dbapi.DB, initial service.Projection, catalog runtimecatalog.Catalog) error {
	t.Helper()
	provider := initial.Providers[0].Active
	provider.ProviderVersion = "1.1.0"
	provider.BundleSHA256 = strings.Repeat("b", 64)
	provider.ManifestSHA256 = provider.BundleSHA256
	provider.ModuleSHA256 = provider.BundleSHA256
	provider.InstallationPath = "fixture/" + provider.BundleSHA256
	target := initial.Providers[0].Targets[0].Target
	extra := target
	extra.ID = "extra"
	provider.Targets = append(provider.Targets, runtimebundle.ActiveTarget{ID: extra.ID, Checkpoint: extra.Checkpoint})
	active := runtimebundle.ActiveDescriptor{
		SchemaVersion: 1, Source: "candidate", SourceTreeSHA256: &provider.BundleSHA256,
		Providers: []runtimebundle.ActiveProvider{provider},
	}
	candidate, err := service.NewProjection(active, map[string]runtimebundle.Manifest{"fixture": {
		SchemaVersion: 1, ProviderID: "fixture", ProviderVersion: provider.ProviderVersion, ProviderAPI: 1,
		ClientModulePath: "client.mjs", Targets: []runtimebundle.Target{extra, target},
	}}, catalog)
	if err != nil {
		return err
	}
	if err := service.New(New(database)).Reconcile(t.Context(), candidate, time.UnixMilli(2)); err != nil {
		return err
	}
	return service.New(New(database)).Reconcile(t.Context(), candidate, time.UnixMilli(3))
}

func TestDeclaredCoreRemovalCannotOrphanUserConfiguration(t *testing.T) {
	database := openProjectionDatabase(t)
	initial := projectionFixture("1.0.0", "a", []string{"state-v1"})
	if err := service.New(New(database.SQL)).Reconcile(t.Context(), initial, time.UnixMilli(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(t.Context(), `INSERT INTO platform_instances(id,platform_id,default_core_id,name,slug,enabled,version,created_at_ms,updated_at_ms) VALUES('custom','gbc','gambatte','My folder','custom',1,1,1,1)`); err != nil {
		t.Fatal(err)
	}
	candidate := projectionFixture("1.1.0", "b", []string{"state-v1"})
	candidate.Definitions.Cores[0].ID = "replacement"
	candidate.Bindings[0].CoreID = "replacement"
	candidate.CatalogSHA256 = strings.Repeat("c", 64)
	if err := service.New(New(database.SQL)).Reconcile(t.Context(), candidate, time.UnixMilli(2)); err == nil {
		t.Fatal("omitting a referenced core was silently accepted")
	}
	var version, core string
	if err := dbapi.QueryRowContext(t.Context(), database.SQL, `SELECT (SELECT provider_version FROM runtime_providers WHERE provider_id='fixture'),default_core_id FROM platform_instances WHERE id='custom'`).Scan(&version, &core); err != nil {
		t.Fatal(err)
	}
	if version != "1.0.0" || core != "gambatte" {
		t.Fatal("failed sync partially changed runtime or user configuration")
	}
}

func TestUnusedProductDefinitionCanBeRemovedWithoutSchemaChange(t *testing.T) {
	database := openProjectionDatabase(t)
	initial := projectionFixture("1.0.0", "a", []string{"state-v1"})
	initial.Definitions.Cores = append([]runtimecatalog.CoreDefinition{{ID: "dormant", Name: "Unused", Enabled: true}}, initial.Definitions.Cores...)
	initial.CatalogSHA256 = strings.Repeat("c", 64)
	if err := service.New(New(database.SQL)).Reconcile(t.Context(), initial, time.UnixMilli(1)); err != nil {
		t.Fatal(err)
	}
	current := projectionFixture("1.0.0", "a", []string{"state-v1"})
	if err := service.New(New(database.SQL)).Reconcile(t.Context(), current, time.UnixMilli(2)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := dbapi.QueryRowContext(t.Context(), database.SQL, `SELECT count(*) FROM cores WHERE id='dormant'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("unused omitted definition remained active")
	}
}
