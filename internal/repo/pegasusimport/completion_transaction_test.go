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

func completionDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db := workflowDatabase(t)
	if _, err := db.ExecContext(t.Context(), `UPDATE jobs SET state='RUNNING',attempt_count=1,finished_at_ms=NULL WHERE id='work';
UPDATE pegasus_imports SET state='RUNNING',completed_at_ms=NULL WHERE id='import-0';`); err != nil {
		t.Fatal(err)
	}
	return db
}

func completionChange(ctx context.Context, records pegasusimportmodel.CompletionRecords) (pegasusimportmodel.CompletionChange, error) {
	before, err := records.Current(ctx, "work")
	if err != nil {
		return pegasusimportmodel.CompletionChange{}, err
	}
	counts, err := records.Counts(ctx, before.ImportID)
	return pegasusimportmodel.CompletionChange{Before: before, Counts: counts, ImportState: "PARTIAL_FAILURE", Retryable: true, NowMS: 10}, err
}

func TestCompletionTransactionRollsBackTerminalPayloadAndEvent(t *testing.T) {
	t.Parallel()
	db := completionDatabase(t)
	before := workflowRows(t, db)
	cause := errors.New("post completion write failure")
	err := NewCompletion(db).WithCompletion(t.Context(), func(records pegasusimportmodel.CompletionRecords) error {
		change, err := completionChange(t.Context(), records)
		if err != nil {
			return err
		}
		if err := records.Complete(t.Context(), change); err != nil {
			return err
		}
		return cause
	})
	if !errors.Is(err, cause) {
		t.Fatalf("completion cause=%v", err)
	}
	if !reflect.DeepEqual(before, workflowRows(t, db)) {
		t.Fatal("completion left partial parent/job/release/event")
	}
}

func TestCompletionTransactionRejectsStaleOwnerSnapshot(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"job version", "parent version", "execution", "attempt", "worker", "lease", "deadline", "parent state"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			db := completionDatabase(t)
			before := workflowRows(t, db)
			err := NewCompletion(db).WithCompletion(t.Context(), func(records pegasusimportmodel.CompletionRecords) error {
				change, err := completionChange(t.Context(), records)
				if err != nil {
					return err
				}
				invalidateRecovery(&change.Before, field)
				return records.Complete(t.Context(), change)
			})
			if !errors.Is(err, pegasusimportmodel.ErrVersionConflict) {
				t.Fatalf("stale %s: %v", field, err)
			}
			if !reflect.DeepEqual(before, workflowRows(t, db)) {
				t.Fatalf("stale %s changed completion", field)
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
