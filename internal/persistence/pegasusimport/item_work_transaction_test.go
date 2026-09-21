package pegasusimport

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	application "retrom/internal/service/pegasusimport"
)

func itemWorkDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db := recoveryDatabase(t)
	if _, err := db.ExecContext(t.Context(), `UPDATE jobs SET leased_until_ms=100 WHERE id='work'`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestItemWorkRejectsStaleExecutionAndSourceCAS(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"claim", "finish"} {
		t.Run(operation, func(t *testing.T) {
			for _, field := range []string{"job version", "parent version", "execution", "attempt", "worker", "lease", "deadline", "parent state", "item version", "item owner"} {
				t.Run(field, func(t *testing.T) { assertItemWorkFence(t, operation, field) })
			}
		})
	}
}

func assertItemWorkFence(t *testing.T, operation, field string) {
	t.Helper()
	db := itemWorkDatabase(t)
	if operation == "claim" {
		if _, err := db.ExecContext(t.Context(), `UPDATE pegasus_import_items SET execution_state='PENDING' WHERE id='item-0'`); err != nil {
			t.Fatal(err)
		}
	}
	before := workflowRows(t, db)
	err := NewItemWork(db).WithItemWork(t.Context(), func(scope application.ItemWorkScope) error {
		owned, err := scope.Read.Current(t.Context(), "item-0")
		if err != nil {
			return err
		}
		switch field {
		case "item version":
			owned.Item.Version++
		case "item owner":
			owned.Item.ImportID = "foreign"
		default:
			invalidateRecovery(&owned.Execution, field)
		}
		if operation == "claim" {
			return scope.Write.Claim(t.Context(), application.ItemClaim{Before: owned, NowMS: 10})
		}
		return scope.Write.Finish(t.Context(), application.ItemFinish{Before: owned, Outcome: application.ItemOutcome{State: "COMMIT_FAILED", Code: "INTERNAL_ERROR"}, NowMS: 10})
	})
	if !errors.Is(err, application.ErrVersionConflict) {
		t.Fatalf("%s %s error=%v", operation, field, err)
	}
	if !reflect.DeepEqual(before, workflowRows(t, db)) {
		t.Fatalf("%s %s changed rows", operation, field)
	}
}

func TestItemWorkFinishRollsBackStateCountsReleaseAndEvent(t *testing.T) {
	t.Parallel()
	db := itemWorkDatabase(t)
	before := workflowRows(t, db)
	cause := errors.New("late item failure")
	err := NewItemWork(db).WithItemWork(t.Context(), func(scope application.ItemWorkScope) error {
		owned, err := scope.Read.Current(t.Context(), "item-0")
		if err != nil {
			return err
		}
		if err := scope.Write.Finish(t.Context(), application.ItemFinish{Before: owned, Outcome: application.ItemOutcome{State: "COMMIT_FAILED", Code: "INTERNAL_ERROR"}, NowMS: 10}); err != nil {
			return err
		}
		return cause
	})
	if !errors.Is(err, cause) {
		t.Fatalf("finish error=%v", err)
	}
	if !reflect.DeepEqual(before, workflowRows(t, db)) {
		t.Fatal("failed item completion left partial state/release/counts/event")
	}
}

func TestItemWorkFinishRecordsCurrentOutcomeOnce(t *testing.T) {
	t.Parallel()
	db := itemWorkDatabase(t)
	service := application.NewItemWork(NewItemWork(db), func() time.Time { return time.UnixMilli(10) })
	identity := application.ExecutionIdentity{JobID: "work", ImportID: "import-0", WorkerID: "old-worker", ExecutionNo: 1, Attempt: 1}
	outcome := application.ItemOutcome{State: "READ_FAILED", Code: "READ_FAILED", Retryable: true}
	if err := service.Finish(t.Context(), identity, "item-0", outcome); err != nil {
		t.Fatal(err)
	}
	var state, code string
	var failed, events int64
	if err := db.QueryRowContext(t.Context(), `SELECT item.execution_state,item.error_code,plan.failed_item_count,
(SELECT count(*) FROM job_events WHERE job_id='work' AND event_type='PROGRESS')
FROM pegasus_import_items item JOIN pegasus_imports plan ON plan.id=item.import_id WHERE item.id='item-0'`).Scan(&state, &code, &failed, &events); err != nil {
		t.Fatal(err)
	}
	if state != "READ_FAILED" || code != "READ_FAILED" || failed != 2 || events != 1 {
		t.Fatalf("outcome %s %s failed=%d events=%d", state, code, failed, events)
	}
	before := workflowRows(t, db)
	if err := service.Finish(t.Context(), identity, "item-0", outcome); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, workflowRows(t, db)) {
		t.Fatal("replayed item completion changed rows")
	}
}
