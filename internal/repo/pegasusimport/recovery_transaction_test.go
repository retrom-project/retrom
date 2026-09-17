package pegasusimport

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	pegasusimportmodel "retrom/internal/model/pegasusimport"
	pegasusimportservice "retrom/internal/service/pegasusimport"
)

func recoveryDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db := workflowDatabase(t)
	if _, err := db.ExecContext(t.Context(), `UPDATE jobs SET state='RUNNING',finished_at_ms=NULL,attempt_count=1,
 leased_until_ms=5,error_code=NULL,error_retryable=0 WHERE id='work';
 UPDATE pegasus_imports SET state='RUNNING',completed_at_ms=NULL,failed_item_count=1,retryable=0 WHERE id='import-0';
 UPDATE pegasus_import_items SET execution_state='COPYING',error_code=NULL,error_details_json=NULL,retryable=0,completed_at_ms=NULL WHERE id='item-0';`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestRecoveryTransactionPreservesExecutionInputAndCompletedItems(t *testing.T) {
	t.Parallel()
	db := recoveryDatabase(t)
	input := workflowTable(t, db, "job_input_snapshots")
	repository := NewRecovery(db)
	service := pegasusimportservice.NewRecovery(repository, nil, func() time.Time { return time.UnixMilli(10) })
	if err := service.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	var state, worker, payload, itemState string
	var execution, attempt, started, deadline, itemVersion int64
	if err := db.QueryRowContext(t.Context(), `SELECT j.state,COALESCE(j.worker_id,''),j.payload_json,j.execution_no,j.attempt_count,
 j.execution_started_at_ms,j.execution_deadline_at_ms,i.execution_state,i.version FROM jobs j JOIN pegasus_import_items i ON i.import_id=j.scope_id
 WHERE j.id='work' AND i.id='item-0'`).Scan(&state, &worker, &payload, &execution, &attempt, &started, &deadline, &itemState, &itemVersion); err != nil {
		t.Fatal(err)
	}
	expected := struct {
		State, Worker, Payload, Item                   string
		Execution, Attempt, Started, Deadline, Version int64
	}{"QUEUED", "", `{"inputExecutionNo":1}`, "PENDING", 1, 1, 1, 100, 2}
	observed := struct {
		State, Worker, Payload, Item                   string
		Execution, Attempt, Started, Deadline, Version int64
	}{state, worker, payload, itemState, execution, attempt, started, deadline, itemVersion}
	if observed != expected {
		t.Fatalf("execution changed: %#v want %#v", observed, expected)
	}

	if workflowTable(t, db, "job_input_snapshots") != input {
		t.Fatal("recovery rewrote frozen input")
	}
	for id, want := range map[string]string{"item-1": "COMMIT_FAILED", "item-2": "REVIEW_PENDING", "item-3": "SKIPPED_MAPPING"} {
		var got string
		if err := db.QueryRowContext(t.Context(), `SELECT execution_state FROM pegasus_import_items WHERE id=?`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("completed item %s=%s want=%s", id, got, want)
		}
	}
	before := workflowRows(t, db)
	if err := service.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, workflowRows(t, db)) {
		t.Fatal("repeated recovery changed queued execution")
	}
}

func TestRecoveryTransactionRejectsStaleOwnership(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"job version", "parent version", "execution", "attempt", "worker", "lease", "deadline", "parent state"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			db := recoveryDatabase(t)
			before := workflowRows(t, db)
			err := NewRecovery(db).WithRecovery(t.Context(), func(scope pegasusimportmodel.RecoveryScope) error {
				current, err := scope.Records.Current(t.Context(), "work")
				if err != nil {
					return err
				}
				change := pegasusimportmodel.RecoveryChange{Before: current, JobState: "QUEUED", ImportState: "QUEUED", ItemState: "PENDING", Event: "RETRY_SCHEDULED", NowMS: 10}
				invalidateRecovery(&change.Before, field)
				return scope.Records.Apply(t.Context(), change)
			})
			if !errors.Is(err, pegasusimportmodel.ErrVersionConflict) {
				t.Fatalf("stale %s: %v", field, err)
			}
			if !reflect.DeepEqual(before, workflowRows(t, db)) {
				t.Fatalf("stale %s wrote projections", field)
			}
		})
	}
}

func invalidateRecovery(before *pegasusimportmodel.RecoverySnapshot, field string) {
	switch field {
	case "job version":
		before.JobVersion++
	case "parent version":
		before.ImportVersion++
	case "execution":
		before.ExecutionNo++
	case "attempt":
		before.Attempt++
	case "worker":
		before.WorkerID = "foreign"
	case "lease":
		before.LeaseUntilMS++
	case "deadline":
		before.DeadlineMS++
	case "parent state":
		before.ImportState = "QUEUED"
	}
}

func TestRecoveryTransactionRollsBackLateFailureIncludingPayloadJobs(t *testing.T) {
	t.Parallel()
	db := recoveryDatabase(t)
	before := workflowRows(t, db)
	cause := errors.New("late write failed")
	err := NewRecovery(db).WithRecovery(t.Context(), func(scope pegasusimportmodel.RecoveryScope) error {
		current, err := scope.Records.Current(t.Context(), "work")
		if err != nil {
			return err
		}
		if err := scope.Records.Apply(t.Context(), pegasusimportmodel.RecoveryChange{Before: current, JobState: "FAILED", ImportState: "FAILED", ItemState: "COMMIT_FAILED", Code: "PEGASUS_EXECUTION_TIMEOUT", Event: "FAILED", NowMS: 100}); err != nil {
			return err
		}
		return cause
	})
	if !errors.Is(err, cause) {
		t.Fatalf("late failure: %v", err)
	}
	if !reflect.DeepEqual(before, workflowRows(t, db)) {
		t.Fatal("failed recovery committed partial projections or payload jobs")
	}
}

func TestRecoveryQueriesPreserveCancellationAndScopeCurrentJob(t *testing.T) {
	t.Parallel()
	db := recoveryDatabase(t)
	if _, err := db.ExecContext(t.Context(), `UPDATE jobs SET state='RUNNING',finished_at_ms=NULL,attempt_count=1,
 leased_until_ms=4,execution_deadline_at_ms=100 WHERE kind='SERVER_PEGASUS_SCAN';`); err != nil {
		t.Fatal(err)
	}
	repository := NewRecovery(db)
	values, err := repository.ExpiredExecutions(t.Context(), 10, 100)
	if err != nil || len(values) != 1 || values[0].JobID != "work" {
		t.Fatalf("old scan selected: %#v %v", values, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := repository.ExpiredExecutions(ctx, 10, 100); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel cause=%v", err)
	}
}
