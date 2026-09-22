package sourceimport

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
	repository "retrom/internal/persistence/sourceimport"
	"retrom/internal/serversource"
	application "retrom/internal/service/sourceimport"
	"retrom/internal/testassert"
)

func arcadeCompanionFixture(t *testing.T) (*Service, work, Root, executionItem) {
	t.Helper()
	setupContext := context.Background()
	dataDir := t.TempDir()
	database, err := sql.Open("sqlite", filepath.Join(dataDir, "companion.db"))
	testassert.False(t, err != nil, err)
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	if _, err := database.ExecContext(setupContext, `
CREATE TABLE source_import_items(id TEXT,import_id TEXT,collection_id TEXT,discovery_state TEXT,execution_state TEXT DEFAULT 'PENDING',version INTEGER DEFAULT 1,library_import_job_id TEXT,library_import_item_id TEXT);
CREATE TABLE jobs(id TEXT,scope_type TEXT,scope_id TEXT,kind TEXT,state TEXT,worker_id TEXT,version INTEGER,execution_no INTEGER,attempt_count INTEGER,max_attempts INTEGER,leased_until_ms INTEGER,execution_deadline_at_ms INTEGER);
CREATE TABLE source_imports(id TEXT,import_job_id TEXT,scan_job_id TEXT,version INTEGER,state TEXT);
INSERT INTO jobs VALUES('work','SOURCE_IMPORT','import','IMPORT_RECEIVE','RUNNING','worker',1,1,1,4,100,1000);
INSERT INTO source_imports VALUES('import','work','scan',1,'RUNNING');
CREATE TABLE source_import_collections(id TEXT,mapping_action TEXT,target_platform_instance_id TEXT,target_dat_version_id TEXT,target_platform_id TEXT);
CREATE TABLE source_import_item_files(item_id TEXT,relative_path TEXT,size_bytes INTEGER,source_facts_digest TEXT,ordinal INTEGER DEFAULT 0,blob_id TEXT);
CREATE TABLE dat_machines(dat_version_id TEXT,machine_name TEXT,cloneof TEXT,romof TEXT);
CREATE TABLE blobs(id TEXT PRIMARY KEY,sha256 TEXT UNIQUE,size_bytes INTEGER,md5 TEXT,sha1 TEXT,crc32 TEXT,media_type TEXT,created_at_ms INTEGER);
INSERT INTO source_import_collections VALUES('collection','IMPORT','target','dat','arcade');
INSERT INTO source_import_items(id,import_id,collection_id,discovery_state) VALUES('parent','import','collection','READY');
INSERT INTO source_import_items(id,import_id,collection_id,discovery_state,execution_state) VALUES('primary','import','collection','READY','COPYING');
INSERT INTO source_import_item_files(item_id,relative_path,size_bytes,source_facts_digest) VALUES('primary','child.zip',1,'child-facts');
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
	testassert.False(t, err != nil, err)
	if _, err := database.ExecContext(
		setupContext,
		`INSERT INTO source_import_item_files(item_id,relative_path,size_bytes,source_facts_digest) VALUES('parent',?,?,?)`,
		name,
		len(contents),
		serversource.FactsDigest(info),
	); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 70; index++ {
		id, candidate := fmt.Sprintf("unrelated-%02d", index), fmt.Sprintf("unrelated-%02d.zip", index)
		if _, err := database.ExecContext(
			setupContext, `INSERT INTO source_import_items(id,import_id,collection_id,discovery_state) VALUES(?,'import','collection','READY')`, id,
		); err != nil {
			t.Fatal(err)
		}
		if _, err := database.ExecContext(
			setupContext, `INSERT INTO source_import_item_files(item_id,relative_path,size_bytes,source_facts_digest) VALUES(?,?,1,'unused')`, id, candidate,
		); err != nil {
			t.Fatal(err)
		}
	}
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	service := &Service{database: database, blobs: blobs, now: func() time.Time { return time.UnixMilli(10) }}
	item := executionItem{
		ID: "primary", TargetPlatformID: "target", TargetDATVersionID: "dat",
		Files: []executionFile{{Path: "child.zip"}},
	}
	unit := work{JobID: "work", ImportID: "import", WorkerID: "worker", ExecutionNo: 1, Attempt: 1}
	return service, unit, Root{path: sourceDir}, item
}

func TestArcadeCompanionsReleaseQueryBeforeRecordingCASBlob(t *testing.T) {
	t.Parallel()
	service, unit, root, item := arcadeCompanionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	const name = "parent.zip"
	candidates, err := application.NewCompanions(repository.NewCompanions(service.database), service.now).Find(
		ctx,
		unit.Identity(),
		item.ID,
	)
	testassert.Falsef(
		t,
		testassert.Any(
			func() bool { return err != nil },
			func() bool { return len(candidates) != 1 },
			func() bool { return candidates[0].File.Path != name },
		),
		"candidates = %#v, error=%v",
		candidates,
		err,
	)
	companions, err := service.importExecutor(root).CompanionFiles(ctx, unit, item)
	testassert.False(t, err != nil, err)
	testassert.Falsef(
		t,
		testassert.Any(
			func() bool { return len(companions) != 1 },
			func() bool { return companions[0].RelativePath != name },
			func() bool { return companions[0].BlobID == "" },
		),
		"companions = %#v",
		companions,
	)
}

func TestLibraryImportFailureExposesSourceFileLimit(t *testing.T) {
	t.Parallel()
	files := make([]libraryimport.ServerSourceFile, libraryimport.ServerSourceFileLimit+1)
	files[0].RelativePath = "1944j.zip"
	details := (&Service{}).libraryImportFailure(libraryimport.ErrInvalid, files)
	testassert.Falsef(
		t,
		testassert.Any(
			func() bool { return details.CauseCode != "SOURCE_FILE_LIMIT_EXCEEDED" },
			func() bool { return details.ObservedFileCount == nil },
			func() bool { return *details.ObservedFileCount != int64(len(files)) },
			func() bool { return details.AllowedFileCount == nil },
			func() bool { return *details.AllowedFileCount != libraryimport.ServerSourceFileLimit },
			func() bool { return details.RelativePath == nil },
			func() bool { return *details.RelativePath != "1944j.zip" },
		),
		"failure details = %#v",
		details,
	)
}

func TestItemFailureKeepsInternalIdentityAndRedactsHostPath(t *testing.T) {
	t.Parallel()
	service := &Service{}
	details := service.itemFailure(
		"RESULT_ATTACHMENT",
		"ATTACH_LIBRARY_RESULT",
		&application.ReviewPreparationError{
			LibraryJobID:  "11111111-1111-4111-8111-111111111111",
			LibraryItemID: "22222222-2222-4222-8222-222222222222",
			Cause:         fmt.Errorf("attach failed: %w", &os.PathError{Op: "open", Path: "/srv/private/library.db", Err: os.ErrPermission}),
		},
		"arcade/1944j.zip",
	)
	testassert.Falsef(
		t,
		testassert.Any(
			func() bool { return strings.Contains(details.TechnicalDetail, "/srv/private") },
			func() bool { return !strings.Contains(details.TechnicalDetail, "[path]") },
		),
		"technical detail = %q",
		details.TechnicalDetail,
	)
	testassert.Falsef(
		t,
		testassert.Any(
			func() bool { return details.LibraryImportJobID == nil },
			func() bool { return details.LibraryImportItemID == nil },
			func() bool { return *details.LibraryImportJobID != "11111111-1111-4111-8111-111111111111" },
			func() bool { return *details.LibraryImportItemID != "22222222-2222-4222-8222-222222222222" },
		),
		"failure details = %#v",
		details,
	)
}

func TestItemFailureClassifiesSQLiteConstraintByDriverCode(t *testing.T) {
	t.Parallel()
	database, err := sql.Open("sqlite", ":memory:")
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	if _, err := database.ExecContext(
		context.Background(),
		`CREATE TABLE unique_value(value TEXT UNIQUE); INSERT INTO unique_value VALUES('same')`,
	); err != nil {
		t.Fatal(err)
	}
	_, constraintError := database.ExecContext(context.Background(), `INSERT INTO unique_value VALUES('same')`)
	testassert.False(t, constraintError == nil, "expected SQLite constraint error")
	details := (&Service{}).itemFailure("STORAGE", "WRITE", constraintError, "")
	testassert.Falsef(t, details.CauseCode != "DATABASE_CONSTRAINT_FAILED", "failure details = %#v", details)
}
