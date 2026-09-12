package pegasusimport

import (
	"testing"

	"retrom/internal/libraryimport"
)

func TestReviewHandoffRejectsReplacedWorker(t *testing.T) {
	t.Parallel()
	service, unit, item := handoffFixture(t)
	unit.WorkerID = "pegasus-import-worker"
	mustExecPegasusTest(t.Context(), t, service.database, `UPDATE jobs SET worker_id='new-worker' WHERE id='work'`)
	service.prepareLibraryReview(t.Context(), unit, item, "handoff-job", libraryimport.ServerImportItem{ItemID: "handoff-item"})
	assertHandoffDraftUntouched(t, service)
}

func TestItemCompletionRejectsReplacedWorker(t *testing.T) {
	t.Parallel()
	service, unit, item := handoffFixture(t)
	mustExecPegasusTest(t.Context(), t, service.database, `UPDATE jobs SET worker_id='new-worker' WHERE id='work'`)
	service.closeItem(t.Context(), unit, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true)
	var state string
	if err := service.database.QueryRowContext(t.Context(), `SELECT execution_state FROM pegasus_import_items WHERE id='item'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "VALIDATING" {
		t.Fatalf("previous worker closed current item: %s", state)
	}
}

func TestClaimNextItemRejectsPreviousExecution(t *testing.T) {
	t.Parallel()
	service, unit, _ := handoffFixture(t)
	mustExecPegasusTest(t.Context(), t, service.database, `UPDATE jobs SET execution_no=2,attempt_count=2 WHERE id='work';
UPDATE pegasus_import_items SET execution_state='PENDING',library_import_job_id=NULL,library_import_item_id=NULL WHERE id='item';`)
	_, found, err := service.nextItem(t.Context(), unit)
	if err == nil || found {
		t.Fatalf("stale worker claimed item: found=%v err=%v", found, err)
	}
}
