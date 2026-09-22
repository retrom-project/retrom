package sourceimport

import "testing"

func TestRecoveryClosesQueuedExecutionWithSpentBudget(t *testing.T) {
	t.Parallel()
	for _, exhausted := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "attempts"}[exhausted], func(t *testing.T) {
			service := prepareLeaseRegression(t)
			code := "SOURCE_EXECUTION_TIMEOUT"
			if exhausted {
				mustExecSourceTest(t.Context(), t, service.database, `UPDATE jobs SET attempt_count=max_attempts WHERE id='work'`)
				code = "SOURCE_WORKER_ATTEMPTS_EXHAUSTED"
			} else {
				mustExecSourceTest(t.Context(), t, service.database, `UPDATE jobs SET execution_deadline_at_ms=10 WHERE id='work'`)
			}
			if err := service.recoverWork(t.Context()); err != nil {
				t.Fatal(err)
			}
			var state, actual string
			if err := service.database.QueryRowContext(t.Context(), `SELECT state,COALESCE(error_code,'') FROM jobs WHERE id='work'`).Scan(&state, &actual); err != nil {
				t.Fatal(err)
			}
			if state != "FAILED" || actual != code {
				t.Fatalf("queued expired execution remains %s: code=%s", state, actual)
			}
		})
	}
}

func TestQueuedRecoveryKeepsAlreadyCreatedReview(t *testing.T) {
	t.Parallel()
	service, _, _ := handoffFixture(t)
	mustExecSourceTest(t.Context(), t, service.database, `UPDATE jobs SET state='QUEUED',worker_id=NULL,leased_until_ms=NULL,
heartbeat_at_ms=NULL,execution_deadline_at_ms=10 WHERE id='work';
UPDATE source_imports SET state='QUEUED' WHERE id='import';`)
	if err := service.recoverWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	after := readHandoffState(t, service)
	if after.State != "REVIEW_PENDING" || after.Pending != 1 || after.DraftVersion != 2 {
		t.Fatalf("queued recovery hid existing review: %+v", after)
	}
	if err := service.recoverWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	if repeated := readHandoffState(t, service); repeated != after {
		t.Fatalf("repeated recovery changed review: %+v", repeated)
	}
}
