package pegasusimport

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	application "retrom/internal/model/pegasusimport"
	"retrom/internal/testkit/testsupport"
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

	repo := NewWorkerSettlement(db)
	current, err := repo.CurrentSettlement(t.Context(), "work")
	if err != nil {
		t.Fatal(err)
	}
	invalidateRecovery(&current, field)
	err = repo.CommitSettlement(t.Context(), application.WorkerSettlementChange{
		Before:  current,
		State:   state,
		Failure: application.ExecutionFailure{Code: "INTERNAL_ERROR", Retryable: true},
		NowMS:   10,
	})
	if !errors.Is(err, application.ErrVersionConflict) {
		t.Fatalf("%s %s=%v", state, field, err)
	}
	if !reflect.DeepEqual(before, workflowRows(t, db)) {
		t.Fatalf("%s %s changed rows", state, field)
	}
}

func TestWorkerSettlementCommitFailureRollsBack(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"FAILED", "CANCELLED"} {
		t.Run(state, func(t *testing.T) {
			assertWorkerSettlementCommitRollback(t, state)
		})
	}
}

func assertWorkerSettlementCommitRollback(t *testing.T, state string) {
	t.Helper()
	db := workerSettlementDatabase(t, state == "CANCELLED")
	before := workflowRows(t, db)

	cause := errors.New("settlement commit injected")
	faultDB := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, _ []driver.NamedValue) error {
			if strings.TrimSpace(query) == "COMMIT" {
				return cause
			}
			return nil
		},
	})

	repo := NewWorkerSettlement(faultDB)
	failure := application.ExecutionFailure{}
	if state == "FAILED" {
		failure = application.ExecutionFailure{Code: "INTERNAL_ERROR", Retryable: true}
	}

	current, err := repo.CurrentSettlement(t.Context(), "work")
	if err != nil {
		t.Fatal(err)
	}
	err = repo.CommitSettlement(t.Context(), application.WorkerSettlementChange{
		Before: current, State: state, Failure: failure, NowMS: 10,
	})
	if err == nil {
		t.Fatal("expected commit failure")
	}
	if !reflect.DeepEqual(before, workflowRows(t, db)) {
		t.Fatalf("%s left settlement writes", state)
	}
}
