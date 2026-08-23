//go:build integration

package libraryimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
	"retrom/internal/uploads"
)

func TestReviewBulkApprovalPublishesStrictReadyCandidatesAtomically(t *testing.T) {
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
	const (
		profileID = "01990000-0000-7000-8000-00000000b710"
		adminID   = "01990000-0000-7000-8000-00000000b711"
	)
	platformInstanceID := testsupport.MustPlatformInstanceID(t, database.SQL, "gba/mgba")
	if _, err := database.SQL.ExecContext(
		ctx, `INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,'Bulk Review Admin',1)`, profileID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,?,'bulk.review.admin','Bulk Review Admin','ADMIN','ENABLED',1,1)
`, adminID, profileID); err != nil {
		t.Fatal(err)
	}
	ctx = authn.WithPrincipal(ctx, authn.Principal{UserID: adminID, ProfileID: profileID, Role: "ADMIN"})
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	uploader := uploads.New(database.SQL, blobs, dataDir, time.Now)
	importer := New(database.SQL, time.Now).WithBlobStore(blobs)
	createImport := func(name, contents string) string {
		t.Helper()
		payload := []byte(contents)
		upload, createErr := uploader.Create(ctx, uploads.CreateRequest{
			SourceType: "FILES",
			Files: []uploads.FileDeclaration{{
				ClientFileID: "game", RelativePath: name, SizeBytes: int64(len(payload)),
			}},
		})
		testassert.False(t, createErr != nil, createErr)
		digest := sha256.Sum256(payload)
		if err := uploader.PutPart(
			ctx, upload.ID, upload.Files[0].ID, 0,
			fmt.Sprintf("bytes 0-%d/%d", len(payload)-1, len(payload)),
			"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(payload),
		); err != nil {
			t.Fatal(err)
		}
		current, err := uploader.Get(ctx, upload.ID)
		testassert.False(t, err != nil, err)
		jobID, _, err := uploader.Complete(ctx, upload.ID, current.Version)
		testassert.False(t, err != nil, err)
		waitForJob(t, database, jobID)
		created, err := importer.Create(ctx, CreateRequest{
			UploadID: upload.ID, TargetPlatformInstanceID: platformInstanceID, MetadataProvider: "NONE",
		})
		testassert.False(t, err != nil, err)
		return created.ImportJobID
	}
	firstImportID := createImport("bulk-first.gba", "bulk-ready-first")
	createImport("bulk-second.gba", "bulk-ready-second")
	preview, err := importer.PreviewReviewBulk(ctx, ReviewBulkScope{})
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return preview.Counts.Matched != 2 }, func() bool { return preview.Counts.StrictReady != 2 }, func() bool { return preview.ActiveBulkApproval != nil }), "preview counts = %#v active=%#v", preview.Counts, preview.ActiveBulkApproval)
	created, err := importer.CreateReviewBulk(ctx, ReviewBulkCreateRequest{
		Scope: preview.Scope, ScopeDigest: preview.ScopeDigest,
		CandidateManifestDigest: preview.CandidateManifestDigest,
	})
	testassert.False(t, err != nil, err)
	deadline := time.Now().Add(5 * time.Second)
	var summary ReviewBulkSummary
	for {
		summary, err = importer.GetReviewBulk(ctx, created.BulkApprovalID)
		testassert.False(t, err != nil, err)
		if summary.State == "COMPLETED" || summary.State == "PARTIAL_FAILURE" || summary.State == "FAILED" {
			break
		}
		testassert.Falsef(t, time.Now().After(deadline), "bulk approval did not finish: %#v", summary)
		time.Sleep(10 * time.Millisecond)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return summary.State != "COMPLETED" }, func() bool { return summary.Progress.Published != 2 }, func() bool { return summary.Progress.Processed != 2 }, func() bool { return summary.Progress.Failed != 0 }), "bulk result = %#v", summary)
	var published, events, bulkItems int
	var auditDiff string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT (SELECT count(*) FROM games WHERE status='PUBLISHED'),
       (SELECT count(*) FROM review_events WHERE event_type='APPROVED'),
       (SELECT count(*) FROM review_bulk_approval_items WHERE bulk_approval_id=? AND state='PUBLISHED'),
       (SELECT diff_json FROM review_events WHERE diff_json LIKE '%QUICK_STRICT_READY%' LIMIT 1)
`, created.BulkApprovalID).Scan(&published, &events, &bulkItems, &auditDiff); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return published != 2 }, func() bool { return events != 2 }, func() bool { return bulkItems != 2 }, func() bool { return !strings.Contains(auditDiff, created.BulkApprovalID) }, func() bool { return !strings.Contains(auditDiff, `"approvalMode":"QUICK_STRICT_READY"`) }), "published/events/items = %d/%d/%d diff=%s", published, events, bulkItems, auditDiff)
	var importState string
	if err := database.SQL.QueryRowContext(ctx, `SELECT state FROM import_jobs WHERE id=?`, firstImportID).
		Scan(&importState); err != nil || importState != "COMPLETED" {
		t.Fatalf("import aggregate = %s, %v", importState, err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
UPDATE review_bulk_approvals SET scope_json='{}' WHERE id=?
`, created.BulkApprovalID); err == nil {
		t.Fatal("bulk approval accepted a frozen scope update")
	}
	if _, err := database.SQL.ExecContext(ctx, `
UPDATE review_bulk_approval_items SET expected_review_version=expected_review_version+1
WHERE bulk_approval_id=? AND import_item_id=(
  SELECT min(import_item_id) FROM review_bulk_approval_items WHERE bulk_approval_id=?
)
`, created.BulkApprovalID, created.BulkApprovalID); err == nil {
		t.Fatal("bulk approval item accepted a frozen review version update")
	}

	createImport("bulk-stale.gba", "bulk-ready-stale")
	stalePreview, err := importer.PreviewReviewBulk(ctx, ReviewBulkScope{})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return stalePreview.Counts.StrictReady != 1 }), "stale preview = %#v, %v", stalePreview, err)
	if _, err := database.SQL.ExecContext(ctx, `
UPDATE review_drafts SET version=version+1,updated_at_ms=updated_at_ms+1
WHERE import_item_id=(SELECT id FROM import_items WHERE state='REVIEW_PENDING' LIMIT 1)
`); err != nil {
		t.Fatal(err)
	}
	if _, err := importer.CreateReviewBulk(ctx, ReviewBulkCreateRequest{
		Scope: stalePreview.Scope, ScopeDigest: stalePreview.ScopeDigest,
		CandidateManifestDigest: stalePreview.CandidateManifestDigest,
	}); !errors.Is(err, ErrReviewBulkPreviewStale) {
		t.Fatalf("stale create error = %v", err)
	}

	insertInterruptedBatch := func(t *testing.T, bulkID, jobID, state string) ReviewBulkSummary {
		t.Helper()
		transaction, err := database.SQL.BeginTx(ctx, nil)
		testassert.False(t, err != nil, err)
		defer cleanup.Rollback(transaction)
		currentPreview, candidates, err := importer.reviewBulkPreviewInTransaction(ctx, transaction, ReviewBulkScope{})
		testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(candidates) != 1 }), "interrupted preview = %#v candidates=%d error=%v", currentPreview, len(candidates), err)
		candidate := candidates[0]
		jobState := state
		workerID := any(nil)
		startedAt := any(nil)
		itemState := "PENDING"
		itemStartedAt := any(nil)
		if state == "RUNNING" {
			workerID = "interrupted-worker"
			startedAt = int64(20)
			itemState = "RUNNING"
			itemStartedAt = int64(20)
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,execution_started_at_ms,worker_id,created_at_ms,updated_at_ms)
VALUES(?,'REVIEW_BULK_APPROVAL',?,'REVIEW_BULK_APPROVE',?,1,'{}',1,?,0,4,1,10,?,?,10,20)
`, jobID, bulkID, fmt.Sprintf("%x", sha256.Sum256([]byte(bulkID))), jobState, startedAt, workerID); err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,1,'{}',?,10)
`, jobID, strings.Repeat("d", 64)); err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_bulk_approvals(id,job_id,state,scope_json,scope_digest,candidate_manifest_digest,
matched_count,candidate_count,screenshot_only_count,duplicate_count,attachment_active_count,
source_flagged_count,not_ready_or_stale_count,created_by_user_id,version,created_at_ms,started_at_ms,updated_at_ms)
VALUES(?,? ,?,'{}',?,?,1,1,0,0,0,0,0,?,1,10,?,20)
`, bulkID, jobID, state, currentPreview.ScopeDigest, currentPreview.CandidateManifestDigest, adminID, startedAt); err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_bulk_approval_items(bulk_approval_id,import_item_id,ordinal,expected_review_version,
expected_validation_id,expected_source_snapshot_id,title_snapshot,target_platform_instance_id,
target_platform_name_snapshot,state,created_at_ms,started_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,10,?)
`, bulkID, candidate.itemID, 0, candidate.reviewVersion, candidate.validationID.String,
			candidate.sourceSnapshotID, strings.TrimSpace(candidate.title), candidate.platformInstanceID,
			candidate.platformName, itemState, itemStartedAt); err != nil {
			t.Fatal(err)
		}
		if err := transaction.Commit(); err != nil {
			t.Fatal(err)
		}
		return ReviewBulkSummary{BulkApprovalID: bulkID, JobID: jobID, State: state, Version: 1}
	}

	restarted := insertInterruptedBatch(t,
		"01990000-0000-7000-8000-00000000b720", "01990000-0000-7000-8000-00000000b721", "RUNNING")
	importer.ResumeReviewBulkJobs(ctx)
	deadline = time.Now().Add(5 * time.Second)
	for {
		summary, err = importer.GetReviewBulk(ctx, restarted.BulkApprovalID)
		testassert.False(t, err != nil, err)
		if summary.State == "COMPLETED" || summary.State == "PARTIAL_FAILURE" || summary.State == "FAILED" {
			break
		}
		testassert.Falsef(t, time.Now().After(deadline), "restarted bulk approval did not finish: %#v", summary)
		time.Sleep(10 * time.Millisecond)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return summary.State != "COMPLETED" }, func() bool { return summary.Progress.Published != 1 }), "restarted result = %#v", summary)
	var gameCount int
	if err := database.SQL.QueryRowContext(ctx, `SELECT count(*) FROM games WHERE status='PUBLISHED'`).Scan(&gameCount); err != nil || gameCount != 3 {
		t.Fatalf("published game count after restart = %d, %v", gameCount, err)
	}

	createImport("bulk-cancel.gba", "bulk-ready-cancel")
	cancelled := insertInterruptedBatch(t,
		"01990000-0000-7000-8000-00000000b722", "01990000-0000-7000-8000-00000000b723", "QUEUED")
	cancelledSummary, err := importer.CancelReviewBulk(ctx, cancelled.BulkApprovalID, cancelled.Version, "integration cancel")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return cancelledSummary.State != "CANCELLED" }, func() bool { return cancelledSummary.Progress.Cancelled != 1 }), "cancelled result = %#v, %v", cancelledSummary, err)
	if err := database.SQL.QueryRowContext(ctx, `SELECT count(*) FROM games WHERE status='PUBLISHED'`).Scan(&gameCount); err != nil || gameCount != 3 {
		t.Fatalf("published game count after cancel = %d, %v", gameCount, err)
	}
}
