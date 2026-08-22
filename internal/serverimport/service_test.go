package serverimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/config"
	"retrom/internal/firmware"
	"retrom/internal/legacychecksum"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/store"
	"retrom/internal/testassert"
)

func TestServerBIOSImportDiscoversAndInstallsExactStaticCandidate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	base := t.TempDir()
	dataDir := filepath.Join(base, "data")
	rootDir := filepath.Join(base, "source")
	if err := os.MkdirAll(rootDir, 0o700); err != nil {
		t.Fatal(err)
	}
	contents := []byte("deterministic server BIOS fixture")
	if err := os.WriteFile(filepath.Join(rootDir, "bios.bin"), contents, 0o600); err != nil {
		t.Fatal(err)
	}
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	defer func() { _ = database.Close() }()
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	testassert.False(t, err != nil, err)
	md5Value, sha1Value := legacychecksum.Sum(contents)
	if _, err := database.SQL.ExecContext(context.Background(), `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('01980000-0000-7000-8000-00000000a001','Admin',1);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES('01980000-0000-7000-8000-00000000b001','01980000-0000-7000-8000-00000000a001','server.admin','Admin','ADMIN','ENABLED',1,1);
INSERT INTO core_artifacts(id,core_id,emulatorjs_version,bundle_version,flavor,relative_path,size_bytes,sha256,
source_commit,provenance_json,compatibility_config_json,enabled,version,created_at_ms,updated_at_ms)
VALUES('fixture-artifact','mgba','4.2.3','fixture','WASM','data/loader.js',1,lower(hex(zeroblob(32))),NULL,'{}','{}',1,1,1,1);
INSERT INTO bios_requirements(id,core_id,core_artifact_id,source_kind,dat_machine_name,logical_name,
requirement_mode,condition_code,activation_options_json,catalog_digest,size_bytes,md5,sha1,sha256,
source_url,source_version,enabled,version,created_at_ms,updated_at_ms,delivery_kind,emulator_path)
VALUES('fixture-requirement','mgba','fixture-artifact','STATIC',NULL,'bios.bin','REQUIRED',NULL,NULL,?,?,?,?,?,
'https://example.invalid/bios','fixture-v1',1,1,1,1,'BIOS_BUNDLE',NULL)
`, fmt.Sprintf("%064x", 2), len(contents), md5Value, sha1Value,
		fmt.Sprintf("%x", sha256.Sum256(contents))); err != nil {
		t.Fatal(err)
	}
	service := New(database.SQL, blobs, firmware.New(database.SQL, time.Now).WithBlobStore(blobs), credentials,
		[]config.ServerImportRoot{{ID: "bios-root", Label: "BIOS Root", Path: rootDir, CanonicalPath: rootDir}}, time.Now)
	created, err := service.Create(ctx, CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, "01980000-0000-7000-8000-00000000b001")
	testassert.False(t, err != nil, err)
	unit, ok, err := service.claim(ctx)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return !ok }), "claim = %#v/%t/%v", unit, ok, err)
	service.execute(ctx, unit)
	var itemState string
	var outcome sql.NullString
	if queryErr := database.SQL.QueryRowContext(context.Background(), `SELECT state,outcome_code FROM server_bios_import_items WHERE server_import_id=?`, created.ID).Scan(&itemState, &outcome); queryErr != nil {
		t.Fatal(queryErr)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return itemState != "IMPORTED_MATCHED" }, func() bool { return !outcome.Valid }, func() bool { return outcome.String != "IMPORTED_MATCHED" }), "item after execute = %s/%v", itemState, outcome)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		created, err = service.Get(ctx, created.ID)
		testassert.False(t, err != nil, err)
		if created.State == "COMPLETED" {
			break
		}
		testassert.Falsef(t, testassert.Any(func() bool { return created.State == "FAILED" }, func() bool { return created.State == "PARTIAL_FAILURE" }), "import = %#v", created)
		time.Sleep(10 * time.Millisecond)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return created.State != "COMPLETED" }, func() bool { return created.Counts.Matched != 1 }), "import = %#v", created)
	var installationID, sourceKind, candidateID, status string
	if err := database.SQL.QueryRowContext(context.Background(), `SELECT id,source_kind,server_import_candidate_id,status FROM bios_installations WHERE is_active=1`).Scan(&installationID, &sourceKind, &candidateID, &status); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return sourceKind != "SERVER_DIRECTORY" }, func() bool { return candidateID == "" }, func() bool { return status != "MATCHED" }), "installation = %s/%s/%s", sourceKind, candidateID, status)

	installationID = verifyServerImportRecovery(ctx, t, service, database.SQL, created)
	verifyServerImportFallbacks(ctx, t, service, database.SQL, rootDir, contents, installationID)
	service.Close()
}

func verifyServerImportRecovery(
	ctx context.Context,
	t *testing.T,
	service *Service,
	database *sql.DB,
	created Summary,
) string {
	t.Helper()
	var rootDigest, catalogDigest string
	if err := database.QueryRowContext(ctx, `
SELECT root_config_digest,catalog_snapshot_digest FROM server_imports WHERE id=?
`, created.ID).Scan(&rootDigest, &catalogDigest); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	if _, err := database.ExecContext(ctx, `
UPDATE server_imports SET state='RUNNING',phase='INSTALLING',completed_at_ms=NULL,updated_at_ms=? WHERE id=?;
UPDATE jobs SET state='RUNNING',finished_at_ms=NULL,leased_until_ms=?,heartbeat_at_ms=?,
worker_id='server-import-worker',updated_at_ms=? WHERE id=?
`, now, created.ID, now+60000, now, now, created.JobID); err != nil {
		t.Fatal(err)
	}
	service.execute(ctx, work{
		ImportID: created.ID, JobID: created.JobID, RootID: "bios-root", RootDigest: rootDigest,
		CatalogDigest: catalogDigest, DeadlineAtMS: now + int64(time.Hour/time.Millisecond),
	})
	var installationCount int
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM bios_installations WHERE requirement_id='fixture-requirement'
`).Scan(&installationCount); err != nil || installationCount != 1 {
		t.Fatalf("recovered installation count = %d, %v", installationCount, err)
	}
	if recovered, err := service.Get(ctx, created.ID); err != nil || recovered.State != "COMPLETED" {
		t.Fatalf("recovered import = %#v, %v", recovered, err)
	}
	sameBytes, err := service.Create(ctx, CreateRequest{
		Kind: "BIOS_DIRECTORY", RootID: "bios-root", ReplaceIfBetter: true,
	}, "01980000-0000-7000-8000-00000000b001")
	testassert.False(t, err != nil, err)
	sameUnit, ok, err := service.claim(ctx)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return !ok }),
		"same-bytes claim = %#v/%t/%v", sameUnit, ok, err)
	service.execute(ctx, sameUnit)
	var sameState string
	if err := database.QueryRowContext(ctx, `
SELECT state FROM server_bios_import_items WHERE server_import_id=?
`, sameBytes.ID).Scan(&sameState); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, sameState != "ALREADY_SAME_BYTES", "same bytes state = %s", sameState)
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM bios_installations WHERE requirement_id='fixture-requirement'
`).Scan(&installationCount); err != nil || installationCount != 1 {
		t.Fatalf("same bytes installation count = %d, %v", installationCount, err)
	}
	if _, err := database.ExecContext(ctx, `
UPDATE bios_requirements SET version=2,catalog_digest=?,updated_at_ms=2 WHERE id='fixture-requirement'
`, fmt.Sprintf("%064x", 3)); err != nil {
		t.Fatal(err)
	}
	_, err = service.Create(ctx, CreateRequest{
		Kind: "BIOS_DIRECTORY", RootID: "bios-root", ReplaceIfBetter: true,
	}, "01980000-0000-7000-8000-00000000b001")
	testassert.False(t, err != nil, err)
	staleUnit, ok, err := service.claim(ctx)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return !ok }),
		"stale-version claim = %#v/%t/%v", staleUnit, ok, err)
	service.execute(ctx, staleUnit)
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM bios_installations WHERE requirement_id='fixture-requirement'
`).Scan(&installationCount); err != nil || installationCount != 2 {
		t.Fatalf("stale version installation count = %d, %v", installationCount, err)
	}
	var installationID string
	if err := database.QueryRowContext(ctx, `
SELECT id FROM bios_installations WHERE requirement_id='fixture-requirement' AND is_active=1
`).Scan(&installationID); err != nil {
		t.Fatal(err)
	}
	return installationID
}

func verifyServerImportFallbacks(
	ctx context.Context,
	t *testing.T,
	service *Service,
	database *sql.DB,
	rootDir string,
	contents []byte,
	installationID string,
) {
	t.Helper()
	if err := os.WriteFile(
		filepath.Join(rootDir, "bios.bin"),
		[]byte("larger but lower-confidence BIOS candidate that must not replace an exact hash"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	downgrade, err := service.Create(ctx, CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root", ReplaceIfBetter: true}, "01980000-0000-7000-8000-00000000b001")
	testassert.False(t, err != nil, err)
	downgradeUnit, ok, err := service.claim(ctx)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return !ok }), "downgrade claim = %#v/%t/%v", downgradeUnit, ok, err)
	service.execute(ctx, downgradeUnit)
	var downgradeState string
	if err := database.QueryRowContext(ctx, `SELECT state FROM server_bios_import_items WHERE server_import_id=?`, downgrade.ID).Scan(&downgradeState); err != nil {
		t.Fatal(err)
	}
	var activeID string
	if err := database.QueryRowContext(ctx, `SELECT id FROM bios_installations WHERE requirement_id='fixture-requirement' AND is_active=1`).Scan(&activeID); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return downgradeState != "SKIPPED_NOT_BETTER" }, func() bool { return activeID != installationID }), "downgrade result = %s, active %s (want %s)", downgradeState, activeID, installationID)
	if err := os.WriteFile(filepath.Join(rootDir, "bios.bin"), contents, 0o600); err != nil {
		t.Fatal(err)
	}

	// Production limits are fixed and cannot be relaxed over HTTP. Shrinking
	// the internal file limit makes the same discovery gate deterministic here:
	// a limit failure must terminalize the task before another installation is
	// committed.
	service.scanLimits.maxFiles = 0
	limited, err := service.Create(ctx, CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, "01980000-0000-7000-8000-00000000b001")
	testassert.False(t, err != nil, err)
	limitedUnit, ok, err := service.claim(ctx)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return !ok }), "scan-limit claim = %#v/%t/%v", limitedUnit, ok, err)
	service.execute(ctx, limitedUnit)
	limitedSummary, err := service.Get(ctx, limited.ID)
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return limitedSummary.State != "FAILED" }, func() bool { return limitedSummary.LastErrorCode == nil }, func() bool { return *limitedSummary.LastErrorCode != "SERVER_IMPORT_SCAN_LIMIT_EXCEEDED" }), "scan-limit import = %#v", limitedSummary)
	var installationCount int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM bios_installations WHERE requirement_id='fixture-requirement'`).Scan(&installationCount); err != nil || installationCount != 2 {
		t.Fatalf("scan-limit installation count = %d, %v", installationCount, err)
	}
	service.scanLimits = defaultScanLimits()

	// A transient unavailable mount schedules a bounded automatic retry rather
	// than prematurely terminalizing every catalog item.
	retrying, err := service.Create(ctx, CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, "01980000-0000-7000-8000-00000000b001")
	testassert.False(t, err != nil, err)
	offline := rootDir + ".offline"
	if err := os.Rename(rootDir, offline); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Rename(offline, rootDir) }()
	retryUnit, ok, err := service.claim(ctx)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return !ok }), "retry claim = %#v/%t/%v", retryUnit, ok, err)
	service.execute(ctx, retryUnit)
	var jobState, importState string
	var attempt int
	if err := database.QueryRowContext(ctx, `SELECT state,attempt_count FROM jobs WHERE id=?`, retrying.JobID).Scan(&jobState, &attempt); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT state FROM server_imports WHERE id=?`, retrying.ID).Scan(&importState); err != nil {
		t.Fatal(err)
	}
	var retryEvents int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM job_events WHERE job_id=? AND event_type='RETRY_SCHEDULED'`, retrying.JobID).Scan(&retryEvents); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return jobState != "QUEUED" }, func() bool { return importState != "QUEUED" }, func() bool { return attempt != 1 }, func() bool { return retryEvents != 1 }), "automatic retry = job %s/import %s/attempt %d/events %d", jobState, importState, attempt, retryEvents)
}

func TestRelativePathAndNoFollowDirectoryBoundary(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"/absolute", "../escape", "a//b", "a\\b", "C:/bios", "a/./b", "a\x00b"} {
		testassert.CheckFalsef(t, ValidateRelativePath(value) == nil, "invalid path accepted: %q", value)
	}
	if err := ValidateRelativePath("合法/bios"); err != nil {
		t.Fatalf("valid path rejected: %v", err)
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := openSelectedDirectory(root, "escape"); !errors.Is(err, ErrPathInvalid) {
		t.Fatalf("symlink error = %v", err)
	}
}

func TestWalkFilesReadsMetadataFromAuthorizedDirectoryDescriptor(t *testing.T) {
	root := t.TempDir()
	selectedPath := filepath.Join(root, "selected")
	if err := os.Mkdir(selectedPath, 0o700); err != nil {
		t.Fatal(err)
	}
	contents := []byte("descriptor-relative BIOS fixture")
	if err := os.WriteFile(filepath.Join(selectedPath, "bios.bin"), contents, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	directory, err := openSelectedDirectory(root, "selected")
	testassert.False(t, err != nil, err)
	defer func() { _ = directory.Close() }()
	visited := make([]discoveredFile, 0, 1)
	counts, err := walkFiles(directory, defaultScanLimits(), func(file discoveredFile) error {
		visited = append(visited, file)
		return nil
	})
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return counts.Files != 1 }, func() bool { return counts.SkippedSpecial != 0 }, func() bool { return len(visited) != 1 }), "walk counts = %#v, visited = %#v", counts, visited)
	testassert.Falsef(t, testassert.Any(func() bool { return visited[0].RelativePath != "bios.bin" }, func() bool { return visited[0].SizeBytes != int64(len(contents)) }), "visited file = %#v", visited[0])
}
