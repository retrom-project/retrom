//go:build integration

package libraryimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
	"retrom/internal/uploads"
)

func TestRetryAndCancelKeepImportItemAggregatesInSync(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	var artifactID string
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT id FROM core_artifacts WHERE core_id='mgba' AND enabled=1
`).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	const (
		retryUploadID  = "01980000-0000-7000-8000-00000000a171"
		retryImportID  = "01980000-0000-7000-8000-00000000b171"
		retryItemID    = "01980000-0000-7000-8000-00000000c171"
		cancelUploadID = "01980000-0000-7000-8000-00000000a172"
		cancelImportID = "01980000-0000-7000-8000-00000000b172"
	)
	digest := strings.Repeat("a", 64)
	for _, uploadID := range []string{retryUploadID, cancelUploadID} {
		if _, err := database.SQL.ExecContext(context.Background(), `
INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,version,
expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'COMPLETE','FILES',1,0,?,1,10000,1000,1000)
`, uploadID, digest); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.SQL.ExecContext(context.Background(), `
INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,
platform_id,default_core_id,core_artifact_id,metadata_provider,config_snapshot_json,config_snapshot_digest,
state,total_item_count,failed_item_count,version,created_at_ms,updated_at_ms)
VALUES(?,?,'01980000-0000-7000-8000-000000000005',1,'gba','mgba',?,'NONE','{}',?,
'PARTIAL_FAILURE',1,1,1,1000,1000)
`, retryImportID, retryUploadID, artifactID, digest); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(context.Background(), `
INSERT INTO import_items(id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,
search_text,failed_stage,last_error_code,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,'FAILED_RETRYABLE','{}',?,'retry.gba','SCRAPING','PROVIDER_TIMEOUT',1,1000,1000)
`, retryItemID, retryImportID, strings.Repeat("b", 64), digest); err != nil {
		t.Fatal(err)
	}
	service := New(database.SQL, func() time.Time { return time.UnixMilli(2_000) })
	retried, err := service.RetryItem(ctx, retryItemID, 1)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return retried.State != "QUEUED" }, func() bool { return retried.Version != 2 }), "retry = %#v, error=%v", retried, err)
	var retryJobState, retryItemState string
	var retryQueued, retryFailed, retryJobVersion, retryEventCount int64
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT job.state,job.queued_item_count,job.failed_item_count,job.version,item.state,
(SELECT count(*) FROM job_events WHERE job_id=? AND event_type='MANUAL_RETRY')
FROM import_jobs job
JOIN import_items item ON item.import_job_id=job.id
WHERE job.id=? AND item.id=?
`, retried.JobID, retryImportID, retryItemID).Scan(
		&retryJobState,
		&retryQueued,
		&retryFailed,
		&retryJobVersion,
		&retryItemState,
		&retryEventCount,
	); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return retryJobState != "RUNNING" }, func() bool { return retryQueued != 1 }, func() bool { return retryFailed != 0 }, func() bool { return retryJobVersion != 2 }, func() bool { return retryItemState != "QUEUED" }, func() bool { return retryEventCount != 1 }), "retry aggregate = job:%s queued:%d failed:%d version:%d item:%s events:%d", retryJobState, retryQueued, retryFailed, retryJobVersion, retryItemState, retryEventCount)

	if _, err := database.SQL.ExecContext(context.Background(), `
INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,
platform_id,default_core_id,core_artifact_id,metadata_provider,config_snapshot_json,config_snapshot_digest,
state,total_item_count,queued_item_count,review_pending_item_count,failed_item_count,version,created_at_ms,updated_at_ms)
VALUES(?,?,'01980000-0000-7000-8000-000000000005',1,'gba','mgba',?,'NONE','{}',?,
'PARTIAL_FAILURE',4,1,1,2,1,1000,1000)
`, cancelImportID, cancelUploadID, artifactID, digest); err != nil {
		t.Fatal(err)
	}
	cancelItems := []struct {
		id        string
		state     string
		stage     any
		errorCode any
	}{
		{"01980000-0000-7000-8000-00000000c172", "QUEUED", nil, nil},
		{"01980000-0000-7000-8000-00000000c173", "REVIEW_PENDING", nil, nil},
		{"01980000-0000-7000-8000-00000000c174", "FAILED_RETRYABLE", "SCRAPING", "PROVIDER_TIMEOUT"},
		{"01980000-0000-7000-8000-00000000c175", "FAILED_FINAL", "IDENTIFYING", "UNSUPPORTED_CONTENT"},
	}
	for index, item := range cancelItems {
		if _, err := database.SQL.ExecContext(context.Background(), `
INSERT INTO import_items(id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,
search_text,failed_stage,last_error_code,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,'{}',?,'cancel.gba',?,?,1,1000,1000)
`, item.id, cancelImportID, strings.Repeat(fmt.Sprintf("%x", index+1), 64), item.state, digest,
			item.stage, item.errorCode); err != nil {
			t.Fatal(err)
		}
	}
	cancelled, pending, err := service.Cancel(ctx, cancelImportID, 1, "operator cancelled")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return pending }, func() bool { return cancelled.State != "CANCELLED" }, func() bool { return cancelled.Version != 2 }), "cancel = %#v pending=%t error=%v", cancelled, pending, err)
	var cancelState string
	var cancelQueued, cancelReviewPending, cancelFailed, cancelCount, cancelCompletedAt int64
	var cancelledItems, failedFinalItems int64
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT state,queued_item_count,review_pending_item_count,failed_item_count,cancelled_item_count,completed_at_ms,
(SELECT count(*) FROM import_items WHERE import_job_id=import_jobs.id AND state='CANCELLED'),
(SELECT count(*) FROM import_items WHERE import_job_id=import_jobs.id AND state='FAILED_FINAL')
FROM import_jobs WHERE id=?
`, cancelImportID).Scan(
		&cancelState,
		&cancelQueued,
		&cancelReviewPending,
		&cancelFailed,
		&cancelCount,
		&cancelCompletedAt,
		&cancelledItems,
		&failedFinalItems,
	); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return cancelState != "CANCELLED" }, func() bool { return cancelQueued != 0 }, func() bool { return cancelReviewPending != 0 }, func() bool { return cancelFailed != 1 }, func() bool { return cancelCount != 3 }, func() bool { return cancelCompletedAt != 2_000 }, func() bool { return cancelledItems != 3 }, func() bool { return failedFinalItems != 1 }), "cancel aggregate = job:%s queued:%d pending:%d failed:%d cancelled:%d completed:%d items:%d/%d", cancelState, cancelQueued, cancelReviewPending, cancelFailed, cancelCount, cancelCompletedAt, cancelledItems, failedFinalItems)
}

func TestDuplicateContentIsSkippedDuringIdentificationAndConfirmedDuringReview(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	uploader := uploads.New(database.SQL, blobs, dataDir, time.Now)
	importer := New(database.SQL, time.Now).WithBlobStore(blobs)
	contents := []byte("duplicate-content-identity-fixture")
	const platformInstanceID = "01980000-0000-7000-8000-000000000005"

	createImport := func(name string) (Created, string) {
		t.Helper()
		upload, createErr := uploader.Create(ctx, uploads.CreateRequest{
			SourceType: "FILES",
			Files: []uploads.FileDeclaration{{
				ClientFileID: "game", RelativePath: name, SizeBytes: int64(len(contents)),
			}},
		})
		testassert.False(t, createErr != nil, createErr)
		digest := sha256.Sum256(contents)
		if putErr := uploader.PutPart(
			ctx,
			upload.ID,
			upload.Files[0].ID,
			0,
			fmt.Sprintf("bytes 0-%d/%d", len(contents)-1, len(contents)),
			"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":",
			bytes.NewReader(contents),
		); putErr != nil {
			t.Fatal(putErr)
		}
		current, getErr := uploader.Get(ctx, upload.ID)
		testassert.False(t, getErr != nil, getErr)
		jobID, _, completeErr := uploader.Complete(ctx, upload.ID, current.Version)
		testassert.False(t, completeErr != nil, completeErr)
		waitForJob(t, database, jobID)
		created, importErr := importer.Create(ctx, CreateRequest{
			UploadID: upload.ID, TargetPlatformInstanceID: platformInstanceID, MetadataProvider: "NONE",
		})
		testassert.False(t, importErr != nil, importErr)
		var itemID string
		if queryErr := database.SQL.QueryRowContext(ctx, `
SELECT id FROM import_items WHERE import_job_id=?
`, created.ImportJobID).Scan(&itemID); queryErr != nil {
			t.Fatal(queryErr)
		}
		return created, itemID
	}

	firstImport, firstItemID := createImport("first-name.gba")
	secondImport, secondItemID := createImport("renamed-copy.gba")
	testassert.Falsef(t, testassert.Any(func() bool { return firstImport.State != "REVIEW_PENDING" }, func() bool { return secondImport.State != "REVIEW_PENDING" }), "pre-publish import states = %s/%s", firstImport.State, secondImport.State)
	firstGame, err := importer.Approve(ctx, firstItemID, 1)
	testassert.False(t, err != nil, err)
	duplicates, identityDigest, err := importer.DuplicateGames(ctx, secondItemID)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(duplicates) != 1 }, func() bool { return duplicates[0].GameID != firstGame.GameID }, func() bool { return len(identityDigest) != 64 }), "review duplicates = %#v digest=%s error=%v", duplicates, identityDigest, err)
	if _, err := importer.Approve(ctx, secondItemID, 1); err == nil {
		t.Fatal("duplicate review published without confirmation")
	} else {
		var conflict *DuplicateConflict
		testassert.Falsef(t, testassert.Any(func() bool { return !errors.As(err, &conflict) }, func() bool { return !errors.Is(err, ErrDuplicateContent) }, func() bool { return len(conflict.Games) != 1 }, func() bool { return conflict.Games[0].GameID != firstGame.GameID }), "duplicate approval error = %#v", err)
	}
	secondGame, err := importer.ApproveWithDecision(ctx, secondItemID, 1, ApprovalDecision{
		DuplicatePolicy: "ALLOW_NEW", AcknowledgedGameIDs: []string{firstGame.GameID},
	})
	testassert.False(t, err != nil, err)
	var auditDiff string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT diff_json FROM review_events WHERE id=?
`, secondGame.EventID).Scan(&auditDiff); err != nil ||
		!strings.Contains(auditDiff, `"duplicatePolicy":"ALLOW_NEW"`) ||
		!strings.Contains(auditDiff, firstGame.GameID) ||
		!strings.Contains(auditDiff, identityDigest) {
		t.Fatalf("duplicate audit diff = %s, error=%v", auditDiff, err)
	}

	thirdImport, thirdItemID := createImport("another-wrapper-name.gba")
	testassert.Falsef(t, thirdImport.State != "COMPLETED", "identification duplicate state = %s", thirdImport.State)
	var jobState, itemState string
	var alreadyItems, alreadyFiles, discarded, pending, draftCount, matchCount, gameCount int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT state,already_imported_item_count,already_imported_file_count,
discarded_item_count,review_pending_item_count
FROM import_jobs WHERE id=?
`, thirdImport.ImportJobID).Scan(
		&jobState, &alreadyItems, &alreadyFiles, &discarded, &pending,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.SQL.QueryRowContext(ctx, `
SELECT state,
(SELECT count(*) FROM review_drafts WHERE import_item_id=import_items.id),
(SELECT count(*) FROM import_item_duplicate_matches WHERE import_item_id=import_items.id)
FROM import_items WHERE id=?
`, thirdItemID).Scan(&itemState, &draftCount, &matchCount); err != nil {
		t.Fatal(err)
	}
	if err := database.SQL.QueryRowContext(ctx, `SELECT count(*) FROM games WHERE status='PUBLISHED'`).Scan(&gameCount); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return jobState != "COMPLETED" }, func() bool { return itemState != "DISCARDED" }, func() bool { return alreadyItems != 1 }, func() bool { return alreadyFiles != 1 }, func() bool { return discarded != 1 }, func() bool { return pending != 0 }, func() bool { return draftCount != 0 }, func() bool { return matchCount != 2 }, func() bool { return gameCount != 2 }), "identification projection = job:%s item:%s already:%d/%d discarded:%d pending:%d drafts:%d matches:%d games:%d", jobState, itemState, alreadyItems, alreadyFiles, discarded, pending, draftCount, matchCount, gameCount)
}

func TestImportGroupsSingleArchiveMemberAndReportsEveryFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	rom := []byte("raw-gba-member")
	archive := makeZIP(t, map[string][]byte{"folder/Wrapped.gba": rom, "README.txt": []byte("readme")})
	files := map[string][]byte{
		"Wrapped.zip":        archive,
		"wrong-platform.iso": []byte("raw-psp-content"),
		".DS_Store":          []byte("sidecar"),
	}
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	declarations := make([]uploads.FileDeclaration, 0, len(files))
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		contents := files[path]
		declarations = append(
			declarations,
			uploads.FileDeclaration{ClientFileID: path, RelativePath: path, SizeBytes: int64(len(contents))},
		)
	}
	upload, err := uploadService.Create(ctx, uploads.CreateRequest{SourceType: "FILES", Files: declarations})
	testassert.False(t, err != nil, err)
	fileByPath := make(map[string]uploads.File, len(upload.Files))
	for _, file := range upload.Files {
		fileByPath[file.RelativePath] = file
	}
	for _, path := range paths {
		contents := files[path]
		digest := sha256.Sum256(contents)
		header := "sha-256=:" + base64.StdEncoding.EncodeToString(digest[:]) + ":"
		if err := uploadService.PutPart(ctx, upload.ID, fileByPath[path].ID, 0, fmt.Sprintf("bytes 0-%d/%d", len(contents)-1, len(contents)), header, bytes.NewReader(contents)); err != nil {
			t.Fatal(err)
		}
	}
	current, err := uploadService.Get(ctx, upload.ID)
	testassert.False(t, err != nil, err)
	jobID, _, err := uploadService.Complete(ctx, upload.ID, current.Version)
	testassert.False(t, err != nil, err)
	waitForJob(t, database, jobID)
	importer := New(database.SQL, time.Now).WithBlobStore(blobs)
	created, err := importer.Create(
		ctx,
		CreateRequest{
			UploadID:                 upload.ID,
			TargetPlatformInstanceID: "01980000-0000-7000-8000-000000000005",
			MetadataProvider:         "NONE",
		},
	)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return created.State != "PARTIAL_FAILURE" }, func() bool { return created.ItemCount != 1 }), "archive import = %#v, error=%v", created, err)
	var source, ignored, rejected, itemCount int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT
(SELECT count(*)
FROM import_job_files
WHERE import_job_id=?
AND disposition='SOURCE'),
(SELECT count(*)
FROM import_job_files
WHERE import_job_id=?
AND disposition='IGNORED'
AND reason_code='IGNORED_SYSTEM_SIDECAR'),
(SELECT count(*)
FROM import_job_files
WHERE import_job_id=?
AND disposition='REJECTED'
AND reason_code='UNSUPPORTED_CONTENT_FORMAT'),
(SELECT count(*)
FROM import_items
WHERE import_job_id=?)
`, created.ImportJobID, created.ImportJobID, created.ImportJobID, created.ImportJobID).Scan(
		&source,
		&ignored,
		&rejected,
		&itemCount,
	); err != nil || source != 1 || ignored != 1 || rejected != 1 || itemCount != 1 {
		t.Fatalf("file dispositions/items = %d/%d/%d/%d, error=%v", source, ignored, rejected, itemCount, err)
	}
	var itemID, logicalName, contentSHA, archiveSHA string
	var archiveOrdinal int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT i.id,
s.logical_name,
b.sha256,
archive.sha256,
s.source_archive_entry_ordinal
FROM import_items i
JOIN import_item_source_files s ON s.import_item_id=i.id
JOIN blobs b ON b.id=s.blob_id
JOIN blobs archive ON archive.id=s.source_archive_blob_id
WHERE i.import_job_id=?
`, created.ImportJobID).Scan(&itemID, &logicalName, &contentSHA, &archiveSHA, &archiveOrdinal); err != nil {
		t.Fatal(err)
	}
	romDigest := sha256.Sum256(rom)
	archiveDigest := sha256.Sum256(archive)
	testassert.Falsef(t, testassert.Any(func() bool { return logicalName != "Wrapped.gba" }, func() bool { return contentSHA != fmt.Sprintf("%x", romDigest) }, func() bool { return archiveSHA != fmt.Sprintf("%x", archiveDigest) }, func() bool { return archiveOrdinal < 0 }), "archive source = %s %s %s %d", logicalName, contentSHA, archiveSHA, archiveOrdinal)
	approved, err := importer.Approve(ctx, itemID, 1)
	testassert.False(t, err != nil, err)
	var publishedLogical, publishedSHA string
	var sourceArchive string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT f.logical_name,
b.sha256,
f.source_archive_blob_id
FROM games g
JOIN game_content_files f ON f.game_content_revision_id=g.current_content_revision_id
JOIN blobs b ON b.id=f.blob_id
WHERE g.id=?
`, approved.GameID).Scan(&publishedLogical, &publishedSHA, &sourceArchive); err != nil ||
		publishedLogical != "Wrapped.gba" ||
		publishedSHA != fmt.Sprintf("%x", romDigest) ||
		sourceArchive == "" {
		t.Fatalf("published archive member = %s/%s/%s, error=%v", publishedLogical, publishedSHA, sourceArchive, err)
	}
	var finalState string
	var completedAt sql.NullInt64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT state,
completed_at_ms
FROM import_jobs
WHERE id=?
`, created.ImportJobID).Scan(&finalState, &completedAt); err != nil ||
		finalState != "PARTIAL_FAILURE" ||
		completedAt.Valid {
		t.Fatalf("import with rejected evidence finalized as %s/%v, error=%v", finalState, completedAt, err)
	}
	var sourceVersion int64
	if err := database.SQL.QueryRowContext(ctx, `SELECT version FROM import_jobs WHERE id=?`, created.ImportJobID).Scan(&sourceVersion); err != nil {
		t.Fatal(err)
	}
	reconfigured, err := importer.Reconfigure(ctx, created.ImportJobID, sourceVersion, ReconfigureRequest{
		TargetPlatformInstanceID: "01980000-0000-7000-8000-000000000023",
		MetadataProvider:         "NONE",
	})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return reconfigured.State != "REVIEW_PENDING" }, func() bool { return reconfigured.ItemCount != 1 }), "reconfigured import = %#v, error=%v", reconfigured, err)
	var sourceState, replacementSource, replacementLogicalName, sourceBlobSHA, replacementBlobSHA string
	var resolvedRejected int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT source.state,
source.resolved_rejected_file_count,
replacement.reconfigured_from_import_job_id,
replacement_file.logical_name,
source_blob.sha256,
replacement_blob.sha256
FROM import_jobs source
JOIN import_jobs replacement ON replacement.id=?
JOIN import_items replacement_item ON replacement_item.import_job_id=replacement.id
JOIN import_item_source_files replacement_file ON replacement_file.import_item_id=replacement_item.id
JOIN blobs replacement_blob ON replacement_blob.id=replacement_file.blob_id
JOIN import_job_file_resolutions resolution ON resolution.import_job_id=source.id
AND resolution.replacement_import_job_id=replacement.id
JOIN upload_files source_file ON source_file.id=resolution.upload_file_id
JOIN blobs source_blob ON source_blob.id=source_file.final_blob_id
WHERE source.id=?
`, reconfigured.ImportJobID, created.ImportJobID).Scan(
		&sourceState,
		&resolvedRejected,
		&replacementSource,
		&replacementLogicalName,
		&sourceBlobSHA,
		&replacementBlobSHA,
	); err != nil || sourceState != "COMPLETED" || resolvedRejected != 1 ||
		replacementSource != created.ImportJobID || replacementLogicalName != "wrong-platform.iso" ||
		sourceBlobSHA != replacementBlobSHA {
		t.Fatalf(
			"reconfiguration evidence = %s/%d/%s/%s/%s/%s, error=%v",
			sourceState,
			resolvedRejected,
			replacementSource,
			replacementLogicalName,
			sourceBlobSHA,
			replacementBlobSHA,
			err,
		)
	}
	if _, err := importer.Reconfigure(ctx, created.ImportJobID, sourceVersion, ReconfigureRequest{
		TargetPlatformInstanceID: "01980000-0000-7000-8000-000000000023",
		MetadataProvider:         "NONE",
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("stale reconfiguration error = %v", err)
	}
}
