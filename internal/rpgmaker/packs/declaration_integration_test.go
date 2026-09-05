//go:build integration

package packs

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/runtimecatalog"
	"retrom/internal/testsupport"
	"retrom/internal/uploads"
)

func TestDeclaredPackInstallsByIdentityUsingExistingLayout(t *testing.T) {
	ctx := t.Context()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	userID := seedRuntimePackUser(t, ctx, database.SQL)
	contents, err := os.ReadFile("../../../data/runtime-target-bindings/v1/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := runtimecatalog.ParseCatalog(contents)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Definitions.AssetPacks = append(catalog.Definitions.AssetPacks, runtimecatalog.AssetPackDefinition{
		ID: "extra-2000-assets", Kind: "ADDITIONAL_ASSETS", Generation: "RPG2000",
		DeclaredName: "Extra Assets", NormalizedDeclaredName: "extra assets", DisplayName: "Extra assets",
		RequiredLayoutVersion: "easy-rtp-layout-v1", Enabled: true,
	})
	transaction, err := database.SQL.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback() }()
	if err := runtimecatalog.SynchronizeDefinitions(ctx, transaction, catalog, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	uploadID := completeRuntimePackDirectory(t, ctx, database.SQL, uploadService)
	var request InstallRequest
	if err := json.Unmarshal([]byte(`{"definitionId":"extra-2000-assets"}`), &request); err != nil {
		t.Fatal(err)
	}
	request.UploadID, request.CreatorID = uploadID, userID
	service := New(database.SQL, blobs, nil, time.Now)
	accepted, err := service.Install(ctx, request)
	if err != nil {
		t.Fatalf("install newly declared pack: %v", err)
	}
	waitForRuntimePackJob(t, ctx, database.SQL, accepted.JobID, "SUCCEEDED")
	var definitionID, status string
	if err := database.SQL.QueryRowContext(ctx, `SELECT definition_id,status FROM runtime_asset_pack_installations WHERE id=?`, accepted.InstallationID).Scan(&definitionID, &status); err != nil {
		t.Fatal(err)
	}
	if definitionID != "extra-2000-assets" || status != "READY" {
		t.Fatalf("installation = %s %s", definitionID, status)
	}
	assertInstalledDefinitionIsProtected(t, database.SQL, catalog)
}

func assertInstalledDefinitionIsProtected(t *testing.T, database *sql.DB, catalog runtimecatalog.Catalog) {
	t.Helper()
	index := len(catalog.Definitions.AssetPacks) - 1
	catalog.Definitions.AssetPacks[index].DisplayName = "Updated display only"
	if err := synchronizePackTestCatalog(t, database, catalog); err != nil {
		t.Fatal(err)
	}
	if err := synchronizePackTestCatalog(t, database, catalog); err != nil {
		t.Fatalf("idempotent installed definition: %v", err)
	}
	catalog.Definitions.AssetPacks[index].Generation = "RPG2003"
	if err := synchronizePackTestCatalog(t, database, catalog); err == nil {
		t.Fatal("installed pack identity changed")
	}
	catalog.Definitions.AssetPacks = catalog.Definitions.AssetPacks[:index]
	if err := synchronizePackTestCatalog(t, database, catalog); err == nil {
		t.Fatal("installed pack definition removed")
	}
	var display, generation, status string
	if err := database.QueryRowContext(t.Context(), `
SELECT definition.display_name,definition.generation,installation.status
FROM runtime_asset_pack_definitions definition JOIN runtime_asset_pack_installations installation
ON installation.definition_id=definition.id WHERE definition.id='extra-2000-assets'
`).Scan(&display, &generation, &status); err != nil {
		t.Fatal(err)
	}
	if display != "Updated display only" || generation != "RPG2000" || status != "READY" {
		t.Fatalf("installed data drifted: %s %s %s", display, generation, status)
	}
}

func synchronizePackTestCatalog(t *testing.T, database *sql.DB, catalog runtimecatalog.Catalog) error {
	t.Helper()
	transaction, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	if err := runtimecatalog.SynchronizeDefinitions(t.Context(), transaction, catalog, time.Now().UnixMilli()); err != nil {
		return err
	}
	return transaction.Commit()
}
