package emulationstationimport

import (
	"errors"
	"testing"
	"time"
)

func TestWorkflowRetryPreservesHandedOffReviewAndPayload(t *testing.T) {
	fixture := newLifecycleFixture(t)
	started, unit := startLifecycleImport(t, fixture, "", "nes")
	item, found, err := fixture.service.nextItem(fixture.context, started.ID)
	if err != nil || !found {
		t.Fatalf("next item found=%v error=%v", found, err)
	}
	fixture.service.closeItem(fixture.context, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true, "")
	fixture.service.execute(fixture.context, unit)
	before, err := fixture.service.Get(fixture.context, started.ID)
	if err != nil || !before.Retryable || before.Counts.ReviewPending != 1 || before.Counts.Failed != 1 {
		t.Fatalf("retryable result=%#v error=%v", before, err)
	}
	var reviewID, jobID, payload, metadata string
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT library_import_item_id,library_import_job_id,payload_state,metadata_json
FROM emulationstation_import_items WHERE import_id=? AND execution_state='REVIEW_PENDING'`, started.ID).Scan(&reviewID, &jobID, &payload, &metadata); err != nil {
		t.Fatal(err)
	}
	*fixture.now = fixture.now.Add(8 * 24 * time.Hour)
	after, err := fixture.service.Retry(fixture.context, started.ID, before.Version, fixture.userID)
	if err != nil || after.Counts.ReviewPending != 1 || after.Counts.Failed != 0 {
		t.Fatalf("retry=%#v error=%v", after, err)
	}
	var unchanged bool
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT library_import_job_id=? AND payload_state=? AND metadata_json=?
FROM emulationstation_import_items WHERE import_id=? AND library_import_item_id=? AND execution_state='REVIEW_PENDING'`, jobID, payload, metadata, started.ID, reviewID).Scan(&unchanged); err != nil {
		t.Fatal(err)
	}
	if !unchanged {
		t.Fatal("retry replaced existing review or its retained source payload")
	}
}

func TestWorkflowRetryCannotDisplaceAnotherActiveImport(t *testing.T) {
	fixture, failed := workflowRetryFixture(t)
	second := mapLifecycleCollection(t, fixture, fixture.createAndScan(t))
	if _, err := fixture.service.StartImport(fixture.context, second.ID, second.Version); err != nil {
		t.Fatal(err)
	}
	result, err := fixture.service.Retry(fixture.context, failed.ID, failed.Version, fixture.userID)
	if result.ID != "" || !errors.Is(err, ErrActive) {
		t.Fatalf("capacity retry result=%#v error=%v", result, err)
	}
	assertRetryWasNotQueued(t, fixture, failed)
}

func TestWorkflowRunningCancellationPreservesLeaseAndPendingItems(t *testing.T) {
	fixture := newLifecycleFixture(t)
	started, _ := startLifecycleImport(t, fixture, "", "nes")
	before, err := fixture.service.Get(fixture.context, started.ID)
	if err != nil {
		t.Fatal(err)
	}
	var lease int64
	var worker string
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT leased_until_ms,worker_id FROM jobs WHERE id=?`, *started.ImportJobID).Scan(&lease, &worker); err != nil {
		t.Fatal(err)
	}
	result, pending, err := fixture.service.Cancel(fixture.context, before.ID, before.Version, "Stop", fixture.userID)
	if err != nil || !pending || result.State != "CANCEL_REQUESTED" || result.Counts != before.Counts {
		t.Fatalf("running cancel=%#v pending=%v error=%v", result, pending, err)
	}
	var unchanged bool
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT leased_until_ms=? AND worker_id=? AND finished_at_ms IS NULL
AND (SELECT count(*) FROM emulationstation_import_items WHERE import_id=? AND execution_state='PENDING')=?
FROM jobs WHERE id=?`, lease, worker, before.ID, before.Counts.Games, *before.ImportJobID).Scan(&unchanged); err != nil {
		t.Fatal(err)
	}
	if !unchanged {
		t.Fatal("pending cancellation changed worker lease or terminalized items early")
	}
}
