package pegasusimport

import (
	"testing"
)

func TestRecoveryPreservesCreatedReviewAcrossExpiredExecution(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"cancel", "timeout", "exhausted"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			service, _, _ := handoffFixture(t)
			mustExecPegasusTest(t.Context(), t, service.database, `UPDATE jobs SET leased_until_ms=5 WHERE id='work'`)
			expected := "FAILED"
			switch mode {
			case "cancel":
				expected = "CANCELLED"
				mustExecPegasusTest(t.Context(), t, service.database, `UPDATE jobs SET state='CANCEL_REQUESTED',cancel_requested_at_ms=3,cancel_reason='Stop' WHERE id='work';
 UPDATE pegasus_imports SET state='CANCEL_REQUESTED',cancel_reason='Stop' WHERE id='import';`)
			case "timeout":
				mustExecPegasusTest(t.Context(), t, service.database, `UPDATE jobs SET execution_deadline_at_ms=5 WHERE id='work'`)
			case "exhausted":
				mustExecPegasusTest(t.Context(), t, service.database, `UPDATE jobs SET attempt_count=max_attempts WHERE id='work'`)
			}
			if err := service.recoverWork(t.Context()); err != nil {
				t.Fatal(err)
			}
			after := readHandoffState(t, service)
			if after.State != "REVIEW_PENDING" || after.Pending != 1 || after.DraftVersion != 2 || after.Search != "changed" || after.Audits != 1 {
				t.Fatalf("%s hid created review: %#v", mode, after)
			}
			var parent, job string
			if err := service.database.QueryRowContext(t.Context(), `SELECT p.state,j.state FROM pegasus_imports p JOIN jobs j ON j.id=p.import_job_id WHERE p.id='import'`).Scan(&parent, &job); err != nil {
				t.Fatal(err)
			}
			if parent != expected || job != expected {
				t.Fatalf("%s recovery parent=%s job=%s", mode, parent, job)
			}
		})
	}
}
