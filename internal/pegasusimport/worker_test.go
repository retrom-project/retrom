package pegasusimport

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/libraryimport"
	"retrom/internal/serversource"
)

func TestSelectServerImportItemUsesTheDeclaredPrimarySource(t *testing.T) {
	t.Parallel()
	items := []libraryimport.ServerImportItem{
		{ItemID: "companion", SourceRelativePaths: []string{"arcade/neogeo.zip"}},
		{ItemID: "primary", SourceRelativePaths: []string{"arcade/mslug.zip"}},
	}
	selected, ok := selectServerImportItem(items, []executionFile{{Path: "arcade/mslug.zip"}})
	if !ok || selected.ItemID != "primary" {
		t.Fatalf("selected = %#v, %v", selected, ok)
	}
	if _, ok := selectServerImportItem(append(items, items[1]), []executionFile{{Path: "arcade/mslug.zip"}}); ok {
		t.Fatal("ambiguous primary source must not be selected")
	}
}

func TestArcadeCompanionsReleaseQueryBeforeRecordingCASBlob(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	dataDir := t.TempDir()
	database, err := sql.Open("sqlite", filepath.Join(dataDir, "companion.db"))
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	if _, err := database.ExecContext(ctx, `
CREATE TABLE pegasus_import_items(id TEXT,import_id TEXT,collection_id TEXT,discovery_state TEXT);
CREATE TABLE pegasus_import_collections(id TEXT,mapping_action TEXT,target_platform_instance_id TEXT);
CREATE TABLE pegasus_import_item_files(item_id TEXT,relative_path TEXT,size_bytes INTEGER,source_facts_digest TEXT);
CREATE TABLE blobs(id TEXT PRIMARY KEY,sha256 TEXT UNIQUE,size_bytes INTEGER,md5 TEXT,sha1 TEXT,crc32 TEXT,media_type TEXT,created_at_ms INTEGER);
INSERT INTO pegasus_import_collections VALUES('collection','IMPORT','target');
INSERT INTO pegasus_import_items VALUES('companion','import','collection','READY');
`); err != nil {
		t.Fatal(err)
	}
	sourceDir := t.TempDir()
	const name = "companion.zip"
	contents := []byte("deterministic arcade companion")
	sourcePath := filepath.Join(sourceDir, name)
	if err := os.WriteFile(sourcePath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(
		ctx,
		`INSERT INTO pegasus_import_item_files VALUES('companion',?,?,?)`,
		name,
		len(contents),
		serversource.FactsDigest(info),
	); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{database: database, blobs: blobs, now: time.Now}
	companions, err := service.arcadeCompanions(
		ctx,
		work{ImportID: "import"},
		Root{path: sourceDir},
		executionItem{ID: "primary", TargetPlatformID: "target"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(companions) != 1 || companions[0].RelativePath != name || companions[0].BlobID == "" {
		t.Fatalf("companions = %#v", companions)
	}
}
