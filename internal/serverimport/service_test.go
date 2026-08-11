package serverimport

import (
	"context"
	"crypto/md5"  //nolint:gosec // Test fixture catalog checksum.
	"crypto/sha1" //nolint:gosec // Test fixture catalog checksum.
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
	retromruntime "retrom/internal/runtime"
	"retrom/internal/store"
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
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	md5Value := fmt.Sprintf("%x", md5.Sum(contents))   //nolint:gosec // Catalog compatibility checksum.
	sha1Value := fmt.Sprintf("%x", sha1.Sum(contents)) //nolint:gosec // Catalog compatibility checksum.
	if _, err := database.SQL.Exec(`
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
	if err != nil {
		t.Fatal(err)
	}
	unit, ok, err := service.claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim = %#v/%t/%v", unit, ok, err)
	}
	service.execute(ctx, unit)
	var itemState string
	var outcome sql.NullString
	if queryErr := database.SQL.QueryRow(`SELECT state,outcome_code FROM server_bios_import_items WHERE server_import_id=?`, created.ID).Scan(&itemState, &outcome); queryErr != nil {
		t.Fatal(queryErr)
	}
	if itemState != "IMPORTED_MATCHED" || !outcome.Valid || outcome.String != "IMPORTED_MATCHED" {
		t.Fatalf("item after execute = %s/%v", itemState, outcome)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		created, err = service.Get(ctx, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if created.State == "COMPLETED" {
			break
		}
		if created.State == "FAILED" || created.State == "PARTIAL_FAILURE" {
			t.Fatalf("import = %#v", created)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if created.State != "COMPLETED" || created.Counts.Matched != 1 {
		t.Fatalf("import = %#v", created)
	}
	var installationID, sourceKind, candidateID, status string
	if err := database.SQL.QueryRow(`SELECT id,source_kind,server_import_candidate_id,status FROM bios_installations WHERE is_active=1`).Scan(&installationID, &sourceKind, &candidateID, &status); err != nil {
		t.Fatal(err)
	}
	if sourceKind != "SERVER_DIRECTORY" || candidateID == "" || status != "MATCHED" {
		t.Fatalf("installation = %s/%s/%s", sourceKind, candidateID, status)
	}

	// Simulate a process crash after the per-item transaction committed but
	// before the aggregate task was finalized. Recovery must use the persisted
	// candidate projection and must not create a duplicate installation revision.
	var rootDigest, catalogDigest string
	if err := database.SQL.QueryRow(`SELECT root_config_digest,catalog_snapshot_digest FROM server_imports WHERE id=?`, created.ID).Scan(&rootDigest, &catalogDigest); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	if _, err := database.SQL.Exec(`
UPDATE server_imports SET state='RUNNING',phase='INSTALLING',completed_at_ms=NULL,updated_at_ms=? WHERE id=?;
UPDATE jobs SET state='RUNNING',finished_at_ms=NULL,leased_until_ms=?,heartbeat_at_ms=?,worker_id='server-import-worker',updated_at_ms=? WHERE id=?
`, now, created.ID, now+60000, now, now, created.JobID); err != nil {
		t.Fatal(err)
	}
	service.execute(ctx, work{
		ImportID: created.ID, JobID: created.JobID, RootID: "bios-root", RootDigest: rootDigest,
		CatalogDigest: catalogDigest, DeadlineAtMS: now + int64(time.Hour/time.Millisecond),
	})
	var installationCount int
	if err := database.SQL.QueryRow(`SELECT count(*) FROM bios_installations WHERE requirement_id='fixture-requirement'`).Scan(&installationCount); err != nil || installationCount != 1 {
		t.Fatalf("recovered installation count = %d, %v", installationCount, err)
	}
	if recovered, getErr := service.Get(ctx, created.ID); getErr != nil || recovered.State != "COMPLETED" {
		t.Fatalf("recovered import = %#v, %v", recovered, getErr)
	}
	sameBytes, err := service.Create(ctx, CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root", ReplaceIfBetter: true}, "01980000-0000-7000-8000-00000000b001")
	if err != nil {
		t.Fatal(err)
	}
	sameUnit, ok, err := service.claim(ctx)
	if err != nil || !ok {
		t.Fatalf("same-bytes claim = %#v/%t/%v", sameUnit, ok, err)
	}
	service.execute(ctx, sameUnit)
	var sameState string
	if err := database.SQL.QueryRow(`SELECT state FROM server_bios_import_items WHERE server_import_id=?`, sameBytes.ID).Scan(&sameState); err != nil {
		t.Fatal(err)
	}
	if sameState != "ALREADY_SAME_BYTES" {
		t.Fatalf("same bytes state = %s", sameState)
	}
	if err := database.SQL.QueryRow(`SELECT count(*) FROM bios_installations WHERE requirement_id='fixture-requirement'`).Scan(&installationCount); err != nil || installationCount != 1 {
		t.Fatalf("same bytes installation count = %d, %v", installationCount, err)
	}

	if _, err := database.SQL.Exec(`UPDATE bios_requirements SET version=2,catalog_digest=?,updated_at_ms=2 WHERE id='fixture-requirement'`, fmt.Sprintf("%064x", 3)); err != nil {
		t.Fatal(err)
	}
	_, err = service.Create(ctx, CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root", ReplaceIfBetter: true}, "01980000-0000-7000-8000-00000000b001")
	if err != nil {
		t.Fatal(err)
	}
	staleUnit, ok, err := service.claim(ctx)
	if err != nil || !ok {
		t.Fatalf("stale-version claim = %#v/%t/%v", staleUnit, ok, err)
	}
	service.execute(ctx, staleUnit)
	if err := database.SQL.QueryRow(`SELECT count(*) FROM bios_installations WHERE requirement_id='fixture-requirement'`).Scan(&installationCount); err != nil || installationCount != 2 {
		t.Fatalf("stale version installation count = %d, %v", installationCount, err)
	}
	if err := database.SQL.QueryRow(`SELECT id FROM bios_installations WHERE requirement_id='fixture-requirement' AND is_active=1`).Scan(&installationID); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(rootDir, "bios.bin"), []byte("larger but lower-confidence BIOS candidate that must not replace an exact hash"), 0o600); err != nil {
		t.Fatal(err)
	}
	downgrade, err := service.Create(ctx, CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root", ReplaceIfBetter: true}, "01980000-0000-7000-8000-00000000b001")
	if err != nil {
		t.Fatal(err)
	}
	downgradeUnit, ok, err := service.claim(ctx)
	if err != nil || !ok {
		t.Fatalf("downgrade claim = %#v/%t/%v", downgradeUnit, ok, err)
	}
	service.execute(ctx, downgradeUnit)
	var downgradeState string
	if err := database.SQL.QueryRow(`SELECT state FROM server_bios_import_items WHERE server_import_id=?`, downgrade.ID).Scan(&downgradeState); err != nil {
		t.Fatal(err)
	}
	var activeID string
	if err := database.SQL.QueryRow(`SELECT id FROM bios_installations WHERE requirement_id='fixture-requirement' AND is_active=1`).Scan(&activeID); err != nil {
		t.Fatal(err)
	}
	if downgradeState != "SKIPPED_NOT_BETTER" || activeID != installationID {
		t.Fatalf("downgrade result = %s, active %s (want %s)", downgradeState, activeID, installationID)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "bios.bin"), contents, 0o600); err != nil {
		t.Fatal(err)
	}

	// Production limits are fixed and cannot be relaxed over HTTP. Shrinking
	// the internal file limit makes the same discovery gate deterministic here:
	// a limit failure must terminalize the task before another installation is
	// committed.
	service.scanLimits.maxFiles = 0
	limited, err := service.Create(ctx, CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, "01980000-0000-7000-8000-00000000b001")
	if err != nil {
		t.Fatal(err)
	}
	limitedUnit, ok, err := service.claim(ctx)
	if err != nil || !ok {
		t.Fatalf("scan-limit claim = %#v/%t/%v", limitedUnit, ok, err)
	}
	service.execute(ctx, limitedUnit)
	limitedSummary, err := service.Get(ctx, limited.ID)
	if err != nil {
		t.Fatal(err)
	}
	if limitedSummary.State != "FAILED" || limitedSummary.LastErrorCode == nil || *limitedSummary.LastErrorCode != "SERVER_IMPORT_SCAN_LIMIT_EXCEEDED" {
		t.Fatalf("scan-limit import = %#v", limitedSummary)
	}
	if err := database.SQL.QueryRow(`SELECT count(*) FROM bios_installations WHERE requirement_id='fixture-requirement'`).Scan(&installationCount); err != nil || installationCount != 2 {
		t.Fatalf("scan-limit installation count = %d, %v", installationCount, err)
	}
	service.scanLimits = defaultScanLimits()

	// A transient unavailable mount schedules a bounded automatic retry rather
	// than prematurely terminalizing every catalog item.
	retrying, err := service.Create(ctx, CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, "01980000-0000-7000-8000-00000000b001")
	if err != nil {
		t.Fatal(err)
	}
	offline := rootDir + ".offline"
	if err := os.Rename(rootDir, offline); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Rename(offline, rootDir) }()
	retryUnit, ok, err := service.claim(ctx)
	if err != nil || !ok {
		t.Fatalf("retry claim = %#v/%t/%v", retryUnit, ok, err)
	}
	service.execute(ctx, retryUnit)
	var jobState, importState string
	var attempt int
	if err := database.SQL.QueryRow(`SELECT state,attempt_count FROM jobs WHERE id=?`, retrying.JobID).Scan(&jobState, &attempt); err != nil {
		t.Fatal(err)
	}
	if err := database.SQL.QueryRow(`SELECT state FROM server_imports WHERE id=?`, retrying.ID).Scan(&importState); err != nil {
		t.Fatal(err)
	}
	var retryEvents int
	if err := database.SQL.QueryRow(`SELECT count(*) FROM job_events WHERE job_id=? AND event_type='RETRY_SCHEDULED'`, retrying.JobID).Scan(&retryEvents); err != nil {
		t.Fatal(err)
	}
	if jobState != "QUEUED" || importState != "QUEUED" || attempt != 1 || retryEvents != 1 {
		t.Fatalf("automatic retry = job %s/import %s/attempt %d/events %d", jobState, importState, attempt, retryEvents)
	}
	service.Close()
}

func TestRelativePathAndNoFollowDirectoryBoundary(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"/absolute", "../escape", "a//b", "a\\b", "C:/bios", "a/./b", "a\x00b"} {
		if ValidateRelativePath(value) == nil {
			t.Errorf("invalid path accepted: %q", value)
		}
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
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = directory.Close() }()
	visited := make([]discoveredFile, 0, 1)
	counts, err := walkFiles(directory, defaultScanLimits(), func(file discoveredFile) error {
		visited = append(visited, file)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if counts.Files != 1 || counts.SkippedSpecial != 0 || len(visited) != 1 {
		t.Fatalf("walk counts = %#v, visited = %#v", counts, visited)
	}
	if visited[0].RelativePath != "bios.bin" || visited[0].SizeBytes != int64(len(contents)) {
		t.Fatalf("visited file = %#v", visited[0])
	}
}
