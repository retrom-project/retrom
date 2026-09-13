//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"retrom/internal/repo/dbexec"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/testkit/testsupport"
)

func claimedApprovalBulk(t *testing.T) (deduplicateFixture, reviewBulkWork) {
	t.Helper()
	fixture := newDeduplicateFixture(t)
	fixture.create(t, "Bulk approval first", "Retrom owned bulk first", 1)
	fixture.create(t, "Bulk approval second", "Retrom owned bulk second", 1)
	fixture.execute(t, `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('approval-profile','Approval',1)`)
	fixture.execute(t, `INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
 VALUES('approval-actor','approval-profile','approval.actor','Approval','ADMIN','ENABLED',1,1)`)
	transaction, err := fixture.database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(transaction)
	preview, candidates, err := fixture.service.reviewBulkPreviewInTransaction(t.Context(), transaction, ReviewBulkScope{})
	if err != nil || len(candidates) != 2 {
		t.Fatalf("candidates=%d err=%v", len(candidates), err)
	}
	scopeJSON, _, err := reviewBulkScopeDigest(preview.Scope)
	if err != nil {
		t.Fatal(err)
	}
	created, err := insertReviewBulkRecords(t.Context(), transaction, "approval-actor", preview, candidates, scopeJSON, fixture.service.now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	work, err := fixture.service.claimReviewBulk(t.Context(), created.BulkApprovalID)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, work
}

func claimApprovalBulkRequest(t *testing.T, fixture deduplicateFixture, work reviewBulkWork) application.ReviewApprovalRequest {
	t.Helper()
	item, err := fixture.service.claimReviewBulkItem(t.Context(), work)
	if err != nil {
		t.Fatal(err)
	}
	return application.ReviewApprovalRequest{ItemID: item.itemID, ExpectedVersion: item.reviewVersion, Bulk: &application.BulkPublicationIntent{
		BulkID: work.bulkID, JobID: work.jobID, WorkerID: work.workerID, ValidationID: item.validationID, SourceSnapshotID: item.sourceSnapshotID,
	}}
}

func TestBulkApprovalKeepsEarlierCommitAndRollsBackCurrentPublication(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"bulk progress", "worker fence"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			verifyBulkApprovalRollback(t, stage)
		})
	}
}

func verifyBulkApprovalRollback(t *testing.T, stage string) {
	t.Helper()
	fixture, work := claimedApprovalBulk(t)
	first := claimApprovalBulkRequest(t, fixture, work)
	published, err := fixture.service.reviewApprovals().Approve(t.Context(), first)
	if err != nil || published.GameID == "" {
		t.Fatalf("first=%+v err=%v", published, err)
	}
	second := claimApprovalBulkRequest(t, fixture, work)
	before := approvalDatabaseRows(t, fixture.database)
	cause := errors.New("bulk progress unavailable")
	want := cause
	hits := 0
	if stage == "worker fence" {
		second.Bulk.WorkerID = "stale-worker"
		want = ErrInvalid
	} else {
		fixture.service.database = testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
			BeforeExec: func(_ context.Context, query string, _ []driver.NamedValue) error {
				if strings.Contains(query, "INSERT INTO job_events") && strings.Contains(query, "'REVIEW_BULK_APPROVAL'") && strings.Contains(query, "'PROGRESS'") {
					hits++
					return cause
				}
				return nil
			},
		})
	}
	result, err := fixture.service.reviewApprovals().Approve(t.Context(), second)
	if !errors.Is(err, want) || result != (Approved{}) || (stage == "bulk progress" && hits != 1) {
		t.Fatalf("second=%+v err=%v hits=%d", result, err, hits)
	}
	assertApprovalRowsUnchanged(t, fixture.database, before)
	assertDeduplicateItemState(t, fixture, first.ItemID, "PUBLISHED")
	assertDeduplicateItemState(t, fixture, second.ItemID, "REVIEW_PENDING")
	fixture.service.database = fixture.database
	second.Bulk.WorkerID = work.workerID
	retry, err := fixture.service.reviewApprovals().Approve(t.Context(), second)
	if err != nil || retry.GameID == "" {
		t.Fatalf("retry=%+v err=%v", retry, err)
	}
	assertCompletedBulkPublication(t, fixture, work.bulkID)
}

func assertCompletedBulkPublication(t *testing.T, fixture deduplicateFixture, bulkID string) {
	t.Helper()
	var processed, publishedCount, events int
	err := fixture.database.QueryRowContext(t.Context(), `SELECT processed_count,published_count,
 (SELECT count(*) FROM review_events WHERE event_type='APPROVED') FROM review_bulk_approvals WHERE id=?`, bulkID).
		Scan(&processed, &publishedCount, &events)
	if err != nil {
		t.Fatal(err)
	}
	if processed != 2 || publishedCount != 2 || events != 2 {
		t.Fatalf("processed=%d published=%d events=%d", processed, publishedCount, events)
	}
}
