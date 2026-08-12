package pegasusimport

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestRuntimeBlockCodePreservesLibraryCompatibilityReason(t *testing.T) {
	t.Parallel()
	if code := runtimeBlockCode(libraryimport.ServerImportItem{CompatibilityCode: "LAUNCH_PARENT_MISSING"}); code != "LAUNCH_PARENT_MISSING" {
		t.Fatalf("runtime block code = %q", code)
	}
	if code := runtimeBlockCode(libraryimport.ServerImportItem{}); code != "PEGASUS_RUNTIME_BLOCKED" {
		t.Fatalf("fallback runtime block code = %q", code)
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
CREATE TABLE pegasus_import_collections(id TEXT,mapping_action TEXT,target_platform_instance_id TEXT,target_dat_version_id TEXT);
CREATE TABLE pegasus_import_item_files(item_id TEXT,relative_path TEXT,size_bytes INTEGER,source_facts_digest TEXT);
CREATE TABLE dat_machines(dat_version_id TEXT,machine_name TEXT,cloneof TEXT,romof TEXT);
CREATE TABLE blobs(id TEXT PRIMARY KEY,sha256 TEXT UNIQUE,size_bytes INTEGER,md5 TEXT,sha1 TEXT,crc32 TEXT,media_type TEXT,created_at_ms INTEGER);
INSERT INTO pegasus_import_collections VALUES('collection','IMPORT','target','dat');
INSERT INTO pegasus_import_items VALUES('parent','import','collection','READY');
INSERT INTO dat_machines VALUES('dat','child','parent',NULL),('dat','parent',NULL,NULL);
`); err != nil {
		t.Fatal(err)
	}
	sourceDir := t.TempDir()
	const name = "parent.zip"
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
		`INSERT INTO pegasus_import_item_files VALUES('parent',?,?,?)`,
		name,
		len(contents),
		serversource.FactsDigest(info),
	); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 70; index++ {
		id, candidate := fmt.Sprintf("unrelated-%02d", index), fmt.Sprintf("unrelated-%02d.zip", index)
		if _, err := database.ExecContext(
			ctx, `INSERT INTO pegasus_import_items VALUES(?,'import','collection','READY')`, id,
		); err != nil {
			t.Fatal(err)
		}
		if _, err := database.ExecContext(
			ctx, `INSERT INTO pegasus_import_item_files VALUES(?,?,1,'unused')`, id, candidate,
		); err != nil {
			t.Fatal(err)
		}
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{database: database, blobs: blobs, now: time.Now}
	item := executionItem{
		ID: "primary", TargetPlatformID: "target", TargetDATVersionID: "dat",
		Files: []executionFile{{Path: "child.zip"}},
	}
	candidates, err := service.arcadeCompanionCandidates(ctx, "import", item)
	if err != nil || len(candidates) != 1 || candidates[0].Path != name {
		t.Fatalf("candidates = %#v, error=%v", candidates, err)
	}
	companions, err := service.arcadeCompanions(
		ctx,
		work{ImportID: "import"},
		Root{path: sourceDir},
		item,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(companions) != 1 || companions[0].RelativePath != name || companions[0].BlobID == "" {
		t.Fatalf("companions = %#v", companions)
	}
}

func TestLibraryImportFailureExposesSourceFileLimit(t *testing.T) {
	t.Parallel()
	files := make([]libraryimport.ServerSourceFile, libraryimport.ServerSourceFileLimit+1)
	files[0].RelativePath = "1944j.zip"
	details := (&Service{}).libraryImportFailure(libraryimport.ErrInvalid, files)
	if details.CauseCode != "SOURCE_FILE_LIMIT_EXCEEDED" || details.ObservedFileCount == nil ||
		*details.ObservedFileCount != int64(len(files)) || details.AllowedFileCount == nil ||
		*details.AllowedFileCount != libraryimport.ServerSourceFileLimit || details.RelativePath == nil ||
		*details.RelativePath != "1944j.zip" {
		t.Fatalf("failure details = %#v", details)
	}
}

func TestItemFailureKeepsInternalIdentityAndRedactsHostPath(t *testing.T) {
	t.Parallel()
	service := &Service{}
	details := withLibraryImportIdentity(
		service.itemFailure(
			"RESULT_ATTACHMENT",
			"ATTACH_LIBRARY_RESULT",
			fmt.Errorf("attach failed: %w", &os.PathError{Op: "open", Path: "/srv/private/library.db", Err: os.ErrPermission}),
			"arcade/1944j.zip",
		),
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
	)
	if strings.Contains(details.TechnicalDetail, "/srv/private") || !strings.Contains(details.TechnicalDetail, "[path]") {
		t.Fatalf("technical detail = %q", details.TechnicalDetail)
	}
	if details.LibraryImportJobID == nil || details.LibraryImportItemID == nil ||
		*details.LibraryImportJobID != "11111111-1111-4111-8111-111111111111" ||
		*details.LibraryImportItemID != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("failure details = %#v", details)
	}
}

func TestItemFailureClassifiesSQLiteConstraintByDriverCode(t *testing.T) {
	t.Parallel()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	if _, err := database.Exec(`CREATE TABLE unique_value(value TEXT UNIQUE); INSERT INTO unique_value VALUES('same')`); err != nil {
		t.Fatal(err)
	}
	_, constraintError := database.Exec(`INSERT INTO unique_value VALUES('same')`)
	if constraintError == nil {
		t.Fatal("expected SQLite constraint error")
	}
	details := (&Service{}).itemFailure("STORAGE", "WRITE", constraintError, "")
	if details.CauseCode != "DATABASE_CONSTRAINT_FAILED" {
		t.Fatalf("failure details = %#v", details)
	}
}
