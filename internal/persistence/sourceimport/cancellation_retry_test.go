package sourceimport

import (
	"errors"
	"reflect"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/sourceimport"
)

func TestCancelledSourceCannotBeRestartedByFailureRecoveryOrRetry(t *testing.T) {
	for _, phase := range []string{"backoff", "failure", "restart"} {
		t.Run(phase, func(t *testing.T) { verifyCancelledSourceCannotRestart(t, phase) })
	}
}

func verifyCancelledSourceCannotRestart(t *testing.T, phase string) {
	t.Helper()
	db := queuedLeaseDatabase(t)
	now := int64(10)
	clock := func() time.Time { return time.UnixMilli(now) }
	leases := application.NewLeases(NewLeases(db), clock)
	control := application.NewWorkflowControl(NewWorkflowControl(db), clock)
	settlement := application.NewWorkerSettlement(NewWorkerSettlement(db), nil, clock)
	var work application.Work
	if phase != "backoff" {
		var found bool
		var err error
		work, found, err = leases.Claim(t.Context())
		if err != nil || !found {
			t.Fatalf("claim: %v %v", found, err)
		}
	}
	command := application.JobCancellationRequest{
		JobID: "work", ScopeID: "import-0", Kind: "IMPORT_RECEIVE", Reason: "Stop", ActorID: "actor",
	}
	result, pending, err := control.CancelJob(t.Context(), command)
	if err != nil || pending != (phase != "backoff") {
		t.Fatalf("cancel: %+v %v %v", result, pending, err)
	}
	assertSourceRetryRejected(t, control, db)
	if phase == "failure" {
		if err := settlement.Fail(t.Context(), work.Identity(), application.ExecutionFailure{
			Code: "IO_FAILED", Retryable: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Recreate recovery as after process restart, beyond both lease and deadline.
	now = 101
	if err := application.NewRecovery(NewRecovery(db), nil, clock).Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	result, pending, err = control.CancelJob(t.Context(), command)
	if err != nil || pending || result.State != "CANCELLED" || result.ExecutionNo != 1 {
		t.Fatalf("cancellation lost after %s: %+v %v %v", phase, result, pending, err)
	}
	before := workflowRows(t, db)
	assertSourceRetryRejected(t, control, db)
	if _, found, err := leases.Claim(t.Context()); err != nil || found {
		t.Fatalf("cancelled work claimed: %v %v", found, err)
	}
	if err := application.NewRecovery(NewRecovery(db), nil, clock).Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if phase != "backoff" {
		if err := settlement.Fail(t.Context(), work.Identity(), application.ExecutionFailure{
			Code: "LATE_FAILURE", Retryable: true,
		}); !errors.Is(err, application.ErrVersionConflict) {
			t.Fatalf("late worker was not fenced: %v", err)
		}
	}
	if !reflect.DeepEqual(before, workflowRows(t, db)) {
		t.Fatal("cancelled work changed after retry, recovery or stale worker completion")
	}
}

func assertSourceRetryRejected(t *testing.T, control *application.WorkflowControl, db dbapi.DB) {
	t.Helper()
	var version int64
	if err := dbapi.QueryRowContext(t.Context(), db, "SELECT version FROM source_imports WHERE id='import-0'").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if _, err := control.Retry(t.Context(), "import-0", version, "actor"); !errors.Is(err, application.ErrNotRetryable) {
		t.Fatalf("cancelled source accepted manual retry: %v", err)
	}
}
