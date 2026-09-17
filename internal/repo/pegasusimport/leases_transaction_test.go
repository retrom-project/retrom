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

func queuedLeaseDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db := recoveryDatabase(t)
	if _, err := db.ExecContext(t.Context(), `UPDATE jobs SET state='QUEUED',leased_until_ms=NULL,worker_id=NULL,heartbeat_at_ms=NULL WHERE id='work';
UPDATE pegasus_imports SET state='QUEUED' WHERE id='import-0';`); err != nil {
		t.Fatal(err)
	}
	return db
}

type failingLeaseRepository struct {
	base    *Leases
	failure error
}

func (repository failingLeaseRepository) WithLease(ctx context.Context, work func(pegasusimportmodel.LeaseRecords) error) error {
	return repository.base.WithLease(ctx, func(records pegasusimportmodel.LeaseRecords) error {
		if err := work(records); err != nil {
			return err
		}
		return repository.failure
	})
}

func TestLeaseClaimRollsBackJobParentAndStartedEvent(t *testing.T) {
	t.Parallel()
	db := queuedLeaseDatabase(t)
	before := workflowRows(t, db)
	cause := errors.New("late lease write failure")
	service := pegasusimportservice.NewLeases(failingLeaseRepository{NewLeases(db), cause}, func() time.Time { return time.UnixMilli(10) })
	unit, found, err := service.Claim(t.Context())
	if !errors.Is(err, cause) || found || unit != (pegasusimportmodel.Work{}) {
		t.Fatalf("claim returned %+v %v %v", unit, found, err)
	}
	if !reflect.DeepEqual(before, workflowRows(t, db)) {
		t.Fatal("failed claim left partial execution")
	}
}

func TestLeaseClaimPreservesBudgetAndRenewsCurrentOwner(t *testing.T) {
	t.Parallel()
	db := queuedLeaseDatabase(t)
	service := pegasusimportservice.NewLeases(NewLeases(db), func() time.Time { return time.UnixMilli(10) })
	unit, found, err := service.Claim(t.Context())
	if err != nil || !found || unit.WorkerID == "" || unit.DeadlineAtMS != 100 || unit.Attempt != 2 {
		t.Fatalf("claim=%+v found=%v err=%v", unit, found, err)
	}
	if err := service.Renew(t.Context(), unit.Identity()); err != nil {
		t.Fatal(err)
	}
	var worker, state string
	var deadline, started, lease, events int64
	if err := db.QueryRowContext(t.Context(), `SELECT worker_id,state,execution_deadline_at_ms,execution_started_at_ms,leased_until_ms,
(SELECT count(*) FROM job_events WHERE job_id='work' AND event_type='STARTED') FROM jobs WHERE id='work'`).Scan(&worker, &state, &deadline, &started, &lease, &events); err != nil {
		t.Fatal(err)
	}
	if worker != unit.WorkerID || state != "RUNNING" || deadline != 100 || started != 1 || lease != 100 || events != 1 {
		t.Fatalf("lease %s %s deadline=%d started=%d lease=%d events=%d", worker, state, deadline, started, lease, events)
	}
}

func TestLeaseRenewalSQLRejectsEveryStaleFence(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"job version", "parent version", "execution", "attempt", "worker", "lease", "deadline", "parent state"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			db := queuedLeaseDatabase(t)
			repository := NewLeases(db)
			if _, _, err := pegasusimportservice.NewLeases(repository, func() time.Time { return time.UnixMilli(10) }).Claim(t.Context()); err != nil {
				t.Fatal(err)
			}
			before := workflowRows(t, db)
			err := repository.WithLease(t.Context(), func(records pegasusimportmodel.LeaseRecords) error {
				current, err := records.Current(t.Context(), "work")
				if err != nil {
					return err
				}
				invalidateRecovery(&current, field)
				return records.Renew(t.Context(), pegasusimportmodel.LeaseRenewal{Before: current, NowMS: 10, LeaseUntilMS: 100})
			})
			if !errors.Is(err, pegasusimportmodel.ErrVersionConflict) {
				t.Fatalf("stale %s: %v", field, err)
			}
			if !reflect.DeepEqual(before, workflowRows(t, db)) {
				t.Fatalf("stale %s changed lease", field)
			}
		})
	}
}

func TestQueuedRecoveryClosesBudgetWithoutAnotherClaim(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"deadline", "attempts", "still claimable"} {
		t.Run(scenario, func(t *testing.T) {
			db := queuedLeaseDatabase(t)
			setQueuedRecoveryBudget(t, db, scenario)
			originalInput := queuedExecutionInput(t, db)
			before := workflowRows(t, db)
			service := pegasusimportservice.NewRecovery(NewRecovery(db), nil, func() time.Time { return time.UnixMilli(10) })
			if err := service.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			if scenario == "still claimable" {
				if !reflect.DeepEqual(before, workflowRows(t, db)) {
					t.Fatal("unspent queue changed")
				}
				return
			}
			assertQueuedRecoveryBudget(t, db, scenario)
			if queuedExecutionInput(t, db) != originalInput {
				t.Fatal("queue recovery changed input")
			}
			settled := workflowRows(t, db)
			if err := service.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(settled, workflowRows(t, db)) {
				t.Fatal("queue recovered twice")
			}
		})
	}
}

func setQueuedRecoveryBudget(t *testing.T, db *sql.DB, scenario string) {
	t.Helper()
	var query string
	switch scenario {
	case "deadline":
		query = "UPDATE jobs SET execution_deadline_at_ms=10 WHERE id='work'"
	case "attempts":
		query = "UPDATE jobs SET attempt_count=max_attempts WHERE id='work'"
	default:
		return
	}
	if _, err := db.ExecContext(t.Context(), query); err != nil {
		t.Fatal(err)
	}
}

func assertQueuedRecoveryBudget(t *testing.T, db *sql.DB, scenario string) {
	t.Helper()
	var state, code, itemCode string
	var attempts, started, deadline int64
	if err := db.QueryRowContext(t.Context(), `SELECT state,error_code,attempt_count,execution_started_at_ms,execution_deadline_at_ms FROM jobs WHERE id='work'`).Scan(&state, &code, &attempts, &started, &deadline); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT error_code FROM pegasus_import_items WHERE id='item-0'`).Scan(&itemCode); err != nil {
		t.Fatal(err)
	}
	wantCode, wantAttempts, wantDeadline := "PEGASUS_EXECUTION_TIMEOUT", int64(1), int64(10)
	if scenario == "attempts" {
		wantCode, wantAttempts, wantDeadline = "PEGASUS_WORKER_ATTEMPTS_EXHAUSTED", 4, 100
	}
	if state != "FAILED" || code != wantCode || attempts != wantAttempts || started != 1 || deadline != wantDeadline || itemCode != code {
		t.Fatalf("recovery changed budget: %s %s attempt=%d start=%d end=%d item=%s", state, code, attempts, started, deadline, itemCode)
	}
}

type queuedInputSnapshot struct {
	JSON, Digest       string
	Execution, Created int64
}

func queuedExecutionInput(t *testing.T, db *sql.DB) queuedInputSnapshot {
	t.Helper()
	var result queuedInputSnapshot
	if err := db.QueryRowContext(t.Context(), `SELECT input_json,input_digest,execution_no,created_at_ms FROM job_input_snapshots WHERE job_id='work'`).Scan(&result.JSON, &result.Digest, &result.Execution, &result.Created); err != nil {
		t.Fatal(err)
	}
	return result
}
