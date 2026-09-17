package pegasusimport

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"

	application "retrom/internal/model/pegasusimport"
)

func workerSettlementDatabase(t *testing.T, cancel bool) *sql.DB {
	t.Helper()
	db := itemWorkDatabase(t)
	if cancel {
		if _, err := db.ExecContext(
			t.Context(),
			`UPDATE jobs SET state='CANCEL_REQUESTED',cancel_reason='stop',cancel_requested_at_ms=10 WHERE id='work';
UPDATE pegasus_imports SET state='CANCEL_REQUESTED',cancel_reason='stop' WHERE id='import-0'`,
		); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestWorkerSettlementFencesFailureAndCancellation(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"FAILED", "CANCELLED"} {
		t.Run(state, func(t *testing.T) {
			for _, field := range []string{
				"job version",
				"parent version",
				"execution",
				"attempt",
				"worker",
				"lease",
				"deadline",
				"parent state",
			} {
				t.Run(field, func(t *testing.T) { assertWorkerSettlementFence(t, state, field) })
			}
		})
	}
}

func assertWorkerSettlementFence(t *testing.T, state, field string) {
	t.Helper()
	db := workerSettlementDatabase(t, state == "CANCELLED")
	before := workflowRows(t, db)
	err := NewWorkerSettlement(db).WithSettlement(t.Context(), func(scope application.WorkerSettlementScope) error {
		current, err := scope.Read.Current(t.Context(), "work")
		if err != nil {
			return err
		}
		invalidateRecovery(&current, field)
		return scope.Write.Close(
			t.Context(),
			application.WorkerSettlementChange{
				Before:  current,
				State:   state,
				Failure: application.ExecutionFailure{Code: "INTERNAL_ERROR", Retryable: true},
				NowMS:   10,
			},
		)
	})
	if !errors.Is(err, application.ErrVersionConflict) {
		t.Fatalf("%s %s=%v", state, field, err)
	}
	if !reflect.DeepEqual(before, workflowRows(t, db)) {
		t.Fatalf("%s %s changed rows", state, field)
	}
}

func TestWorkerSettlementRollsBackJobItemsCountsEventAndRelease(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"FAILED", "CANCELLED"} {
		t.Run(state, func(t *testing.T) {
			assertWorkerSettlementRollback(t, state)
		})
	}
}

func assertWorkerSettlementRollback(t *testing.T, state string) {
	t.Helper()
	db := workerSettlementDatabase(t, state == "CANCELLED")
	before := workflowRows(t, db)
	cause := errors.New("failure after worker close")
	err := NewWorkerSettlement(db).WithSettlement(t.Context(), func(scope application.WorkerSettlementScope) error {
		current, err := scope.Read.Current(t.Context(), "work")
		if err != nil {
			return err
		}
		failure := application.ExecutionFailure{}
		if state == "FAILED" {
			failure = application.ExecutionFailure{Code: "INTERNAL_ERROR", Retryable: true}
		}
		if err := scope.Write.Close(
			t.Context(),
			application.WorkerSettlementChange{Before: current, State: state, Failure: failure, NowMS: 10},
		); err != nil {
			return err
		}
		closed, err := scope.Read.Current(t.Context(), "work")
		if err != nil {
			return err
		}
		if closed.JobState != state || closed.ImportState != state {
			t.Fatalf("not actually closed: %#v", closed)
		}
		return cause
	})
	if !errors.Is(err, cause) {
		t.Fatalf("late cause=%v", err)
	}
	if !reflect.DeepEqual(before, workflowRows(t, db)) {
		t.Fatalf("%s left settlement writes", state)
	}
}
