package libraryimport

import (
	"database/sql"
	"testing"

	application "retrom/internal/service/libraryimport"
)

func TestReviewBulkWorkerClaimsCompletesAndFinishesInTypedScope(t *testing.T) {
	t.Parallel()
	database := metadataDatabase(t)
	bulkID, _ := insertReviewBulkQueryFixture(t, database)
	repository := NewReviewBulkWorker(database)
	work := claimReviewBulkWorker(t, repository, bulkID)
	item := claimReviewBulkWorkerItem(t, repository, work)
	completeReviewBulkWorkerItem(t, repository, work, item)
	finishReviewBulkWorker(t, repository, work)
	assertReviewBulkWorkerStates(t, database, bulkID, "PARTIAL_FAILURE", "SUCCEEDED", "FAILED_FINAL")
}

func claimReviewBulkWorker(
	t *testing.T,
	repository *ReviewBulkWorker,
	bulkID string,
) application.ReviewBulkWork {
	t.Helper()
	var work application.ReviewBulkWork
	if err := repository.WithWorker(t.Context(), func(scope application.ReviewBulkWorkerScope) error {
		var err error
		work, err = scope.Claim(t.Context(), application.ReviewBulkClaim{
			BulkApprovalID: bulkID, WorkerID: "worker", NowMS: 10, DeadlineMS: 1000,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if work.BulkApprovalID != bulkID || work.JobID == "" || work.WorkerID != "worker" || work.UserID != "actor" {
		t.Fatalf("claimed work=%#v", work)
	}
	return work
}

func claimReviewBulkWorkerItem(
	t *testing.T,
	repository *ReviewBulkWorker,
	work application.ReviewBulkWork,
) application.ReviewBulkWorkItem {
	t.Helper()
	var item application.ReviewBulkWorkItem
	if err := repository.WithWorker(t.Context(), func(scope application.ReviewBulkWorkerScope) error {
		var err error
		item, err = scope.ClaimItem(t.Context(), work, 11)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if item.ImportItemID != "item" || item.ValidationID != "validation" || item.ExpectedReviewVersion != 7 {
		t.Fatalf("claimed item=%#v", item)
	}
	return item
}

func completeReviewBulkWorkerItem(
	t *testing.T,
	repository *ReviewBulkWorker,
	work application.ReviewBulkWork,
	item application.ReviewBulkWorkItem,
) {
	t.Helper()
	if err := repository.WithWorker(t.Context(), func(scope application.ReviewBulkWorkerScope) error {
		return scope.CompleteItem(t.Context(), application.ReviewBulkItemCompletion{
			Work: work, Item: item, State: "FAILED_FINAL", OutcomeCode: "FAILED",
			DetailsJSON: `{"schemaVersion":1,"code":"FAILED"}`,
			NowMS:       12, LeasedUntilMS: 1012,
		})
	}); err != nil {
		t.Fatal(err)
	}
}

func finishReviewBulkWorker(t *testing.T, repository *ReviewBulkWorker, work application.ReviewBulkWork) {
	t.Helper()
	if err := repository.WithWorker(t.Context(), func(scope application.ReviewBulkWorkerScope) error {
		return scope.Finish(t.Context(), work, 13)
	}); err != nil {
		t.Fatal(err)
	}
}

func assertReviewBulkWorkerStates(
	t *testing.T,
	database *sql.DB,
	bulkID, expectedBulk, expectedJob, expectedItem string,
) {
	t.Helper()
	var bulkState, jobState, itemState string
	if err := database.QueryRowContext(t.Context(), `
SELECT bulk.state,job.state,item.state
FROM review_bulk_approvals bulk
JOIN jobs job ON job.id=bulk.job_id
JOIN review_bulk_approval_items item ON item.bulk_approval_id=bulk.id
WHERE bulk.id=?`, bulkID).Scan(&bulkState, &jobState, &itemState); err != nil {
		t.Fatal(err)
	}
	if bulkState != expectedBulk || jobState != expectedJob || itemState != expectedItem {
		t.Fatalf("states bulk=%s job=%s item=%s", bulkState, jobState, itemState)
	}
}

func TestReviewBulkWorkerCancellationUsesCompareAndSetScope(t *testing.T) {
	t.Parallel()
	database := metadataDatabase(t)
	bulkID, _ := insertReviewBulkQueryFixture(t, database)
	repository := NewReviewBulkWorker(database)
	var target application.ReviewBulkCancelTarget
	if err := repository.WithWorker(t.Context(), func(scope application.ReviewBulkWorkerScope) error {
		var err error
		target, err = scope.LoadCancelTarget(t.Context(), bulkID, 1)
		if err != nil {
			return err
		}
		return scope.RequestCancellation(t.Context(), application.ReviewBulkCancellationRequest{
			Target: target, BulkApprovalID: bulkID, Reason: "test", ExpectedVersion: 1, NowMS: 20,
		})
	}); err != nil {
		t.Fatal(err)
	}
	if target.State != "QUEUED" || target.JobState != "QUEUED" {
		t.Fatalf("target=%#v", target)
	}
	if err := repository.WithWorker(t.Context(), func(scope application.ReviewBulkWorkerScope) error {
		return scope.FinalizeCancellation(t.Context(), bulkID, 21)
	}); err != nil {
		t.Fatal(err)
	}
	var bulkState, jobState, itemState string
	if err := database.QueryRowContext(t.Context(), `
SELECT bulk.state,job.state,item.state
FROM review_bulk_approvals bulk
JOIN jobs job ON job.id=bulk.job_id
JOIN review_bulk_approval_items item ON item.bulk_approval_id=bulk.id
WHERE bulk.id=?`, bulkID).Scan(&bulkState, &jobState, &itemState); err != nil {
		t.Fatal(err)
	}
	if bulkState != "CANCELLED" || jobState != "CANCELLED" || itemState != "CANCELLED" {
		t.Fatalf("states bulk=%s job=%s item=%s", bulkState, jobState, itemState)
	}
}
