package pegasusimport

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	pegasusimportmodel "retrom/internal/model/pegasusimport"
	pegasusimportservice "retrom/internal/service/pegasusimport"
)

func itemWorkDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db := recoveryDatabase(t)
	if _, err := db.ExecContext(t.Context(), `UPDATE jobs SET leased_until_ms=100 WHERE id='work'`); err != nil {
		t.Fatal(err)
	}
	return db
}

func validItemIdentity() pegasusimportmodel.ExecutionIdentity {
	return pegasusimportmodel.ExecutionIdentity{
		JobID: "work", ImportID: "import-0", WorkerID: "old-worker", ExecutionNo: 1, Attempt: 1,
	}
}

func TestItemWorkRejectsStaleIdentity(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"claim", "finish"} {
		t.Run(operation, func(t *testing.T) {
			for _, field := range []string{"worker", "execution", "attempt"} {
				t.Run(field, func(t *testing.T) {
					db := itemWorkDatabase(t)
					if operation == "claim" {
						if _, err := db.ExecContext(t.Context(), `UPDATE pegasus_import_items SET execution_state='PENDING' WHERE id='item-0'`); err != nil {
							t.Fatal(err)
						}
					}
					before := workflowRows(t, db)
					identity := validItemIdentity()
					switch field {
					case "worker":
						identity.WorkerID = "other-worker"
					case "execution":
						identity.ExecutionNo = 999
					case "attempt":
						identity.Attempt = 999
					}
					repo := NewItemWork(db)
					var err error
					if operation == "claim" {
						_, err = repo.ClaimNextItem(t.Context(), identity, 10)
					} else {
						err = repo.CommitItemFinish(t.Context(), identity, "item-0",
							pegasusimportmodel.ItemOutcome{State: "COMMIT_FAILED", Code: "INTERNAL_ERROR"}, 10)
					}
					if !errors.Is(err, pegasusimportmodel.ErrVersionConflict) {
						t.Fatalf("%s %s error=%v", operation, field, err)
					}
					if !reflect.DeepEqual(before, workflowRows(t, db)) {
						t.Fatalf("%s %s changed rows", operation, field)
					}
				})
			}
		})
	}
}

func TestItemWorkRejectsExpiredLease(t *testing.T) {
	t.Parallel()
	db := itemWorkDatabase(t)
	if _, err := db.ExecContext(t.Context(), `UPDATE jobs SET leased_until_ms=1 WHERE id='work'`); err != nil {
		t.Fatal(err)
	}
	before := workflowRows(t, db)
	identity := validItemIdentity()
	err := NewItemWork(db).CommitItemFinish(t.Context(), identity, "item-0",
		pegasusimportmodel.ItemOutcome{State: "COMMIT_FAILED", Code: "INTERNAL_ERROR"}, 10)
	if !errors.Is(err, pegasusimportmodel.ErrVersionConflict) {
		t.Fatalf("expired lease error=%v", err)
	}
	if !reflect.DeepEqual(before, workflowRows(t, db)) {
		t.Fatal("expired lease changed rows")
	}
}

func TestItemWorkRejectsExpiredDeadline(t *testing.T) {
	t.Parallel()
	db := itemWorkDatabase(t)
	if _, err := db.ExecContext(t.Context(), `UPDATE jobs SET execution_deadline_at_ms=1 WHERE id='work'`); err != nil {
		t.Fatal(err)
	}
	before := workflowRows(t, db)
	identity := validItemIdentity()
	err := NewItemWork(db).CommitItemFinish(t.Context(), identity, "item-0",
		pegasusimportmodel.ItemOutcome{State: "COMMIT_FAILED", Code: "INTERNAL_ERROR"}, 10)
	if !errors.Is(err, pegasusimportmodel.ErrVersionConflict) {
		t.Fatalf("expired deadline error=%v", err)
	}
	if !reflect.DeepEqual(before, workflowRows(t, db)) {
		t.Fatal("expired deadline changed rows")
	}
}

func TestItemWorkFinishRecordsCurrentOutcomeOnce(t *testing.T) {
	t.Parallel()
	db := itemWorkDatabase(t)
	service := pegasusimportservice.NewItemWork(NewItemWork(db), func() time.Time { return time.UnixMilli(10) })
	identity := pegasusimportmodel.ExecutionIdentity{JobID: "work", ImportID: "import-0", WorkerID: "old-worker", ExecutionNo: 1, Attempt: 1}
	outcome := pegasusimportmodel.ItemOutcome{State: "READ_FAILED", Code: "READ_FAILED", Retryable: true}
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
