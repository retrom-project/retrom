package sourceimport

import (
	"testing"
	"time"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/sourceimport"
)

func TestExhaustedSourceRecoveryRequiresExplicitNewExecution(t *testing.T) {
	t.Parallel()
	for _, budget := range []string{"attempts", "deadline"} {
		t.Run(budget, func(t *testing.T) {
			db := recoveryDatabase(t)
			setQueuedRecoveryBudget(t, db, budget)
			clock := func() time.Time { return time.UnixMilli(10) }
			if err := application.NewRecovery(NewRecovery(db), nil, clock).Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			var before application.WorkflowSnapshot
			if err := NewWorkflowControl(db).WithControl(t.Context(), func(scope application.WorkflowScope) error {
				var err error
				before, err = scope.Read.Current(t.Context(), "import-0")
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if before.JobState != "FAILED" || !application.RetryAvailable(before) || before.RetryableItems != 1 {
				t.Fatalf("exhaustion lost manual retry: %+v", before)
			}
			if _, found, err := application.NewLeases(NewLeases(db), clock).Claim(t.Context()); err != nil || found {
				t.Fatalf("exhaustion automatically restarted: %v %v", found, err)
			}
			control := application.NewWorkflowControl(NewWorkflowControl(db), clock)
			if _, err := control.Retry(t.Context(), "import-0", before.Summary.Version, "actor"); err != nil {
				t.Fatal(err)
			}
			var state string
			var execution, attempt int64
			if err := dbapi.QueryRowContext(t.Context(), db,
				`SELECT state,execution_no,attempt_count FROM jobs WHERE id='work'`).Scan(&state, &execution, &attempt); err != nil {
				t.Fatal(err)
			}
			if state != "QUEUED" || execution != 2 || attempt != 0 {
				t.Fatalf("manual retry did not start a new execution: %s %d %d", state, execution, attempt)
			}
		})
	}
}
