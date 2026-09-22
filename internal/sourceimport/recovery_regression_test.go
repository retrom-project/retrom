package sourceimport

import (
	"database/sql"
	"testing"
	"time"
)

func recoveryFixture(t *testing.T) *Service {
	t.Helper()
	db := newSourceRetryDatabase(t)
	if _, err := db.ExecContext(t.Context(), `
UPDATE jobs SET state='RUNNING',finished_at_ms=NULL,attempt_count=1,leased_until_ms=5,
execution_started_at_ms=1,execution_deadline_at_ms=100,heartbeat_at_ms=1,worker_id='lost-worker' WHERE id='work';
UPDATE source_imports SET state='RUNNING',phase='COPYING_CONTENT',completed_at_ms=NULL,failed_item_count=0,retryable=0;
UPDATE source_import_items SET execution_state='COPYING',completed_at_ms=NULL,error_code=NULL,error_details_json=NULL,retryable=0;
`); err != nil {
		t.Fatal(err)
	}
	return &Service{database: db, now: func() time.Time { return time.UnixMilli(10) }}
}

func TestRecoveryClosesCancellationAfterWorkerLeaseExpires(t *testing.T) {
	t.Parallel()
	service := recoveryFixture(t)
	if _, err := service.database.ExecContext(t.Context(), `
UPDATE jobs SET state='CANCEL_REQUESTED',cancel_requested_at_ms=3,cancel_reason='Stop' WHERE id='work';
UPDATE source_imports SET state='CANCEL_REQUESTED',cancel_reason='Stop';
`); err != nil {
		t.Fatal(err)
	}
	if err := service.recoverWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	var jobState, planState, itemState string
	if err := service.database.QueryRowContext(t.Context(), `SELECT job.state,plan.state,item.execution_state
FROM source_imports plan JOIN jobs job ON job.id=plan.import_job_id
JOIN source_import_items item ON item.import_id=plan.id WHERE plan.id='import'`).Scan(&jobState, &planState, &itemState); err != nil {
		t.Fatal(err)
	}
	if jobState != "CANCELLED" || planState != "CANCELLED" || itemState != "CANCELLED" {
		t.Fatalf("expired cancellation stuck: job=%s plan=%s item=%s", jobState, planState, itemState)
	}
}

func TestRecoveryPreservesTimeoutReasonOnUnfinishedItems(t *testing.T) {
	t.Parallel()
	service := recoveryFixture(t)
	if _, err := service.database.ExecContext(t.Context(), `UPDATE jobs SET execution_deadline_at_ms=5 WHERE id='work'`); err != nil {
		t.Fatal(err)
	}
	if err := service.recoverWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	var jobCode, itemCode string
	if err := service.database.QueryRowContext(t.Context(), `SELECT job.error_code,item.error_code FROM jobs job
JOIN source_import_items item ON item.import_id=job.scope_id WHERE job.id='work'`).Scan(&jobCode, &itemCode); err != nil {
		t.Fatal(err)
	}
	if jobCode != "SOURCE_EXECUTION_TIMEOUT" || itemCode != jobCode {
		t.Fatalf("timeout reason changed: job=%s item=%s", jobCode, itemCode)
	}
}

func TestRecoveryClosesUnclaimedItemsWhenExecutionIsExhausted(t *testing.T) {
	t.Parallel()
	service := recoveryFixture(t)
	if _, err := service.database.ExecContext(t.Context(), `
UPDATE jobs SET attempt_count=4 WHERE id='work';
UPDATE source_import_items SET execution_state='PENDING';
`); err != nil {
		t.Fatal(err)
	}
	if err := service.recoverWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	var state string
	var code sql.NullString
	var failed int
	if err := service.database.QueryRowContext(t.Context(), `SELECT item.execution_state,item.error_code,plan.failed_item_count
FROM source_import_items item JOIN source_imports plan ON plan.id=item.import_id WHERE item.id='item'`).Scan(&state, &code, &failed); err != nil {
		t.Fatal(err)
	}
	if state != "COMMIT_FAILED" || !code.Valid || code.String != "SOURCE_WORKER_ATTEMPTS_EXHAUSTED" || failed != 1 {
		t.Fatalf("exhausted execution kept pending payload: state=%s code=%#v failed=%d", state, code, failed)
	}
}
