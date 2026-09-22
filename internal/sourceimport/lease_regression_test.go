package sourceimport

import (
	"testing"

	"github.com/google/uuid"
)

func prepareLeaseRegression(t *testing.T) *Service {
	t.Helper()
	service := recoveryFixture(t)
	mustExecSourceTest(t.Context(), t, service.database, `UPDATE jobs SET state='QUEUED',leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL WHERE id='work';
 UPDATE source_imports SET state='QUEUED' WHERE id='import'`)
	return service
}

func TestClaimPreservesOriginalExecutionDeadline(t *testing.T) {
	t.Parallel()
	service := prepareLeaseRegression(t)
	unit, ok, err := service.claim(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("claim failed")
	}
	var deadline int64
	if err := service.database.QueryRowContext(t.Context(), `SELECT execution_deadline_at_ms FROM jobs WHERE id='work'`).Scan(&deadline); err != nil {
		t.Fatal(err)
	}
	if unit.DeadlineAtMS != deadline || deadline != 100 {
		t.Fatalf("claim extended context budget: returned=%d stored=%d", unit.DeadlineAtMS, deadline)
	}
}

func TestClaimDoesNotResurrectTerminalParent(t *testing.T) {
	t.Parallel()
	service := prepareLeaseRegression(t)
	mustExecSourceTest(t.Context(), t, service.database, `UPDATE source_imports SET state='COMPLETED',completed_at_ms=10 WHERE id='import'`)
	if _, ok, _ := service.claim(t.Context()); ok {
		t.Fatal("queued obsolete job resurrected terminal plan")
	}
}

func TestClaimChecksWorkerIdentityEntropy(t *testing.T) {
	service := prepareLeaseRegression(t)
	uuid.SetRand(unavailableCreationEntropy{})
	ok := func() bool { defer uuid.SetRand(nil); _, claimed, _ := service.claim(t.Context()); return claimed }()
	if ok {
		t.Fatal("claim succeeded without valid worker identity entropy")
	}
}

func TestFinishImportRejectsPreviousExecution(t *testing.T) {
	t.Parallel()
	service := recoveryFixture(t)
	mustExecSourceTest(t.Context(), t, service.database, `UPDATE jobs SET execution_no=2,attempt_count=2,leased_until_ms=100 WHERE id='work'`)
	err := service.finishImport(t.Context(), work{JobID: "work", ImportID: "import", ExecutionNo: 1, Attempt: 1})
	var state string
	if scanErr := service.database.QueryRowContext(t.Context(), `SELECT state FROM jobs WHERE id='work'`).Scan(&state); scanErr != nil {
		t.Fatal(scanErr)
	}
	if err == nil || state != "RUNNING" {
		t.Fatalf("stale finalizer closed current execution: state=%s err=%v", state, err)
	}
}
