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

func completionDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db := workflowDatabase(t)
	if _, err := db.ExecContext(t.Context(), `UPDATE jobs SET state='RUNNING',attempt_count=1,finished_at_ms=NULL WHERE id='work';
UPDATE pegasus_imports SET state='RUNNING',completed_at_ms=NULL WHERE id='import-0';`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCompletionTransactionRollsBackTerminalPayloadAndEvent(t *testing.T) {
	t.Parallel()
	db := completionDatabase(t)
	before := workflowRows(t, db)
	if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER fail_completion_event BEFORE INSERT ON job_events
WHEN NEW.event_type='SUCCEEDED' BEGIN SELECT RAISE(ABORT,'injected completion failure'); END`); err != nil {
		t.Fatal(err)
	}
	identity := pegasusimportmodel.ExecutionIdentity{JobID: "work", ImportID: "import-0", WorkerID: "old-worker", ExecutionNo: 1, Attempt: 1}
	err := NewCompletion(db).CommitCompletion(t.Context(), identity, 10)
	if err == nil {
		t.Fatal("completion should have failed")
	}
	if _, err := db.ExecContext(t.Context(), `DROP TRIGGER fail_completion_event`); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, workflowRows(t, db)) {
		t.Fatal("completion left partial parent/job/release/event")
	}
}

func TestCompletionTransactionRejectsStaleOwnerSnapshot(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"worker", "execution", "attempt"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			db := completionDatabase(t)
			before := workflowRows(t, db)
			identity := pegasusimportmodel.ExecutionIdentity{JobID: "work", ImportID: "import-0", WorkerID: "old-worker", ExecutionNo: 1, Attempt: 1}
			switch field {
			case "worker":
				identity.WorkerID = "stale-worker"
			case "execution":
				identity.ExecutionNo = 99
			case "attempt":
				identity.Attempt = 99
			}
			err := NewCompletion(db).CommitCompletion(t.Context(), identity, 10)
			if !errors.Is(err, pegasusimportmodel.ErrVersionConflict) {
				t.Fatalf("stale %s: %v", field, err)
			}
			if !reflect.DeepEqual(before, workflowRows(t, db)) {
				t.Fatalf("stale %s changed completion", field)
			}
		})
	}
}

func TestCompletionTransactionRejectsExpiredLeaseAndDeadline(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"lease", "deadline"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			db := completionDatabase(t)
			switch field {
			case "lease":
				if _, err := db.ExecContext(t.Context(), `UPDATE jobs SET leased_until_ms=5 WHERE id='work'`); err != nil {
					t.Fatal(err)
				}
			case "deadline":
				if _, err := db.ExecContext(t.Context(), `UPDATE jobs SET execution_deadline_at_ms=5 WHERE id='work'`); err != nil {
					t.Fatal(err)
				}
			}
			identity := pegasusimportmodel.ExecutionIdentity{JobID: "work", ImportID: "import-0", WorkerID: "old-worker", ExecutionNo: 1, Attempt: 1}
			err := NewCompletion(db).CommitCompletion(t.Context(), identity, 10)
			if !errors.Is(err, pegasusimportmodel.ErrVersionConflict) {
				t.Fatalf("expired %s: %v", field, err)
			}
		})
	}
}

func TestCompletionCountsAndFinalEventCommitOnlyOnce(t *testing.T) {
	t.Parallel()
	db := completionDatabase(t)
	service := pegasusimportservice.NewCompletion(NewCompletion(db), func() time.Time { return time.UnixMilli(10) })
	identity := pegasusimportmodel.ExecutionIdentity{JobID: "work", ImportID: "import-0", WorkerID: "old-worker", ExecutionNo: 1, Attempt: 1}
	if err := service.Finish(t.Context(), identity); err != nil {
		t.Fatal(err)
	}
	var job, plan string
	var failed, pending, events int64
	if err := db.QueryRowContext(t.Context(), `SELECT job.state,plan.state,plan.failed_item_count,plan.review_pending_item_count,
(SELECT count(*) FROM job_events WHERE job_id='work' AND event_type='SUCCEEDED')
FROM jobs job JOIN pegasus_imports plan ON plan.id=job.scope_id WHERE job.id='work'`).Scan(&job, &plan, &failed, &pending, &events); err != nil {
		t.Fatal(err)
	}
	if job != "SUCCEEDED" || plan != "PARTIAL_FAILURE" || failed != 2 || pending != 1 || events != 1 {
		t.Fatalf("completion %s %s failed=%d pending=%d events=%d", job, plan, failed, pending, events)
	}
	before := workflowRows(t, db)
	if err := service.Finish(t.Context(), identity); !errors.Is(err, pegasusimportmodel.ErrVersionConflict) {
		t.Fatalf("repeated completion=%v", err)
	}
	if !reflect.DeepEqual(before, workflowRows(t, db)) {
		t.Fatal("repeated completion wrote rows")
	}
}
