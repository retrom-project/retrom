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
	"retrom/internal/testsupport"
	"retrom/internal/uploads"
)

func TestReviewBulkApprovalPublishesStrictReadyCandidatesAtomically(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	const (
		profileID          = "01990000-0000-7000-8000-00000000b710"
		adminID            = "01990000-0000-7000-8000-00000000b711"
		platformInstanceID = "01980000-0000-7000-8000-000000000005"
	)
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
	if err != nil {
		t.Fatal(err)
	}
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
		if createErr != nil {
			t.Fatal(createErr)
		}
		digest := sha256.Sum256(payload)
		if err := uploader.PutPart(
			ctx, upload.ID, upload.Files[0].ID, 0,
			fmt.Sprintf("bytes 0-%d/%d", len(payload)-1, len(payload)),
			"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(payload),
		); err != nil {
			t.Fatal(err)
		}
		current, err := uploader.Get(ctx, upload.ID)
		if err != nil {
			t.Fatal(err)
		}
		jobID, _, err := uploader.Complete(ctx, upload.ID, current.Version)
		if err != nil {
			t.Fatal(err)
		}
		waitForJob(t, database, jobID)
		created, err := importer.Create(ctx, CreateRequest{
			UploadID: upload.ID, TargetPlatformInstanceID: platformInstanceID, MetadataProvider: "NONE",
		})
		if err != nil {
			t.Fatal(err)
		}
		return created.ImportJobID
	}
	firstImportID := createImport("bulk-first.gba", "bulk-ready-first")
	createImport("bulk-second.gba", "bulk-ready-second")
	preview, err := importer.PreviewReviewBulk(ctx, ReviewBulkScope{})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Counts.Matched != 2 || preview.Counts.StrictReady != 2 || preview.ActiveBulkApproval != nil {
		t.Fatalf("preview counts = %#v active=%#v", preview.Counts, preview.ActiveBulkApproval)
	}
	created, err := importer.CreateReviewBulk(ctx, ReviewBulkCreateRequest{
		Scope: preview.Scope, ScopeDigest: preview.ScopeDigest,
		CandidateManifestDigest: preview.CandidateManifestDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var summary ReviewBulkSummary
	for {
		summary, err = importer.GetReviewBulk(ctx, created.BulkApprovalID)
		if err != nil {
			t.Fatal(err)
		}
		if summary.State == "COMPLETED" || summary.State == "PARTIAL_FAILURE" || summary.State == "FAILED" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("bulk approval did not finish: %#v", summary)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if summary.State != "COMPLETED" || summary.Progress.Published != 2 || summary.Progress.Processed != 2 ||
		summary.Progress.Failed != 0 {
		t.Fatalf("bulk result = %#v", summary)
	}
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
	if published != 2 || events != 2 || bulkItems != 2 ||
		!strings.Contains(auditDiff, created.BulkApprovalID) ||
		!strings.Contains(auditDiff, `"approvalMode":"QUICK_STRICT_READY"`) {
		t.Fatalf("published/events/items = %d/%d/%d diff=%s", published, events, bulkItems, auditDiff)
	}
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
	if err != nil || stalePreview.Counts.StrictReady != 1 {
		t.Fatalf("stale preview = %#v, %v", stalePreview, err)
	}
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
		if err != nil {
			t.Fatal(err)
		}
		defer cleanup.Rollback(transaction)
		currentPreview, candidates, err := importer.reviewBulkPreviewInTransaction(ctx, transaction, ReviewBulkScope{})
		if err != nil || len(candidates) != 1 {
			t.Fatalf("interrupted preview = %#v candidates=%d error=%v", currentPreview, len(candidates), err)
		}
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
not_ready_or_stale_count,created_by_user_id,version,created_at_ms,started_at_ms,updated_at_ms)
VALUES(?,? ,?,'{}',?,?,1,1,0,0,0,0,?,1,10,?,20)
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
		if err != nil {
			t.Fatal(err)
		}
		if summary.State == "COMPLETED" || summary.State == "PARTIAL_FAILURE" || summary.State == "FAILED" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("restarted bulk approval did not finish: %#v", summary)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if summary.State != "COMPLETED" || summary.Progress.Published != 1 {
		t.Fatalf("restarted result = %#v", summary)
	}
	var gameCount int
	if err := database.SQL.QueryRowContext(ctx, `SELECT count(*) FROM games WHERE status='PUBLISHED'`).Scan(&gameCount); err != nil || gameCount != 3 {
		t.Fatalf("published game count after restart = %d, %v", gameCount, err)
	}

	createImport("bulk-cancel.gba", "bulk-ready-cancel")
	cancelled := insertInterruptedBatch(t,
		"01990000-0000-7000-8000-00000000b722", "01990000-0000-7000-8000-00000000b723", "QUEUED")
	cancelledSummary, err := importer.CancelReviewBulk(ctx, cancelled.BulkApprovalID, cancelled.Version, "integration cancel")
	if err != nil || cancelledSummary.State != "CANCELLED" || cancelledSummary.Progress.Cancelled != 1 {
		t.Fatalf("cancelled result = %#v, %v", cancelledSummary, err)
	}
	if err := database.SQL.QueryRowContext(ctx, `SELECT count(*) FROM games WHERE status='PUBLISHED'`).Scan(&gameCount); err != nil || gameCount != 3 {
		t.Fatalf("published game count after cancel = %d, %v", gameCount, err)
	}
}
