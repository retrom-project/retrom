package payloadrelease

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"retrom/internal/repo/dbexec"
	"retrom/internal/testkit/testsupport"
)

type releaseWorkerFixture struct {
	database *sql.DB
	service  *Service
	jobID    string
	now      *atomic.Int64
}

func queuedReleaseWorker(t *testing.T) releaseWorkerFixture {
	t.Helper()
	db := schedulingGame(t)
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(tx)
	id, err := ScheduleGameDeletion(t.Context(), tx, "schedule-game", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	clock := &atomic.Int64{}
	clock.Store(10)
	service, err := New(db, nil, func() time.Time { return time.UnixMilli(clock.Load()) }, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	return releaseWorkerFixture{database: db, service: service, jobID: id, now: clock}
}

func TestPayloadRecoveryDoesNotStealAnUnexpiredLease(t *testing.T) {
	t.Parallel()
	fixture := queuedReleaseWorker(t)
	claim, found, err := fixture.service.claim(t.Context())
	if err != nil || !found {
		t.Fatalf("claim failed: %v %t", err, found)
	}
	before := releaseJobAuthority(t, fixture.database, claim.ID)
	if err := fixture.service.recoverInterruptedJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	after := releaseJobAuthority(t, fixture.database, claim.ID)
	if after != before {
		t.Fatalf("recovery stole a live execution: before=%+v after=%+v", before, after)
	}
}

type jobAuthority struct {
	State, Worker            string
	Version, Attempt         int64
	Started, Deadline, Lease sql.NullInt64
}

func releaseJobAuthority(t *testing.T, db *sql.DB, id string) jobAuthority {
	t.Helper()
	var result jobAuthority
	err := db.QueryRowContext(t.Context(), `SELECT state,COALESCE(worker_id,''),version,attempt_count,
execution_started_at_ms,execution_deadline_at_ms,leased_until_ms FROM jobs WHERE id=?`, id).
		Scan(&result.State, &result.Worker, &result.Version, &result.Attempt, &result.Started, &result.Deadline, &result.Lease)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPayloadRecoveryPreservesOriginalExecutionDeadline(t *testing.T) {
	t.Parallel()
	fixture := queuedReleaseWorker(t)
	claim, found, err := fixture.service.claim(t.Context())
	if err != nil || !found {
		t.Fatalf("claim failed: %v %t", err, found)
	}
	before := releaseJobAuthority(t, fixture.database, claim.ID)
	if _, err := fixture.database.ExecContext(t.Context(), `UPDATE jobs SET leased_until_ms=11 WHERE id=?`, claim.ID); err != nil {
		t.Fatal(err)
	}
	fixture.now.Store(12)
	if err := fixture.service.recoverInterruptedJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	requeued := releaseJobAuthority(t, fixture.database, claim.ID)
	if requeued.State != "QUEUED" || requeued.Started != before.Started || requeued.Deadline != before.Deadline {
		t.Fatalf("recovery extended execution budget: before=%+v requeued=%+v", before, requeued)
	}
	if _, found, err := fixture.service.claim(t.Context()); err != nil || !found {
		t.Fatalf("recoverable attempt could not resume: %v %t", err, found)
	}
	resumed := releaseJobAuthority(t, fixture.database, claim.ID)
	if resumed.Started != before.Started || resumed.Deadline != before.Deadline || resumed.Attempt != before.Attempt+1 ||
		resumed.Worker == before.Worker {
		t.Fatalf("new attempt lost original execution identity or reused worker: before=%+v after=%+v", before, resumed)
	}
}

func TestPayloadCompletionRejectsAReplacedWorker(t *testing.T) {
	t.Parallel()
	fixture := queuedReleaseWorker(t)
	claim, found, err := fixture.service.claim(t.Context())
	if err != nil || !found {
		t.Fatalf("claim failed: %v %t", err, found)
	}
	if _, err := fixture.database.ExecContext(t.Context(), `UPDATE jobs
SET worker_id='replacement-worker',version=version+1 WHERE id=?`, claim.ID); err != nil {
		t.Fatal(err)
	}
	before := releaseJobAuthority(t, fixture.database, claim.ID)
	err = fixture.service.finish(t.Context(), claim, nil)
	if err == nil || releaseJobAuthority(t, fixture.database, claim.ID) != before {
		t.Fatalf("replaced worker settled another attempt: error=%v after=%+v", err,
			releaseJobAuthority(t, fixture.database, claim.ID))
	}
	var events, audits int
	err = fixture.database.QueryRowContext(t.Context(), `SELECT
(SELECT count(*) FROM job_events WHERE job_id=? AND event_type='SUCCEEDED'),
(SELECT count(*) FROM audit_events WHERE resource_id='schedule-game')`, claim.ID).Scan(&events, &audits)
	if err != nil || events != 0 || audits != 0 {
		t.Fatalf("stale worker emitted terminal evidence: events=%d audits=%d error=%v", events, audits, err)
	}
}

func TestPayloadClaimPreservesAffectedRowFailure(t *testing.T) {
	t.Parallel()
	fixture := queuedReleaseWorker(t)
	cause := errors.New("claim affected rows failed")
	var hits atomic.Int64
	db := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.HasPrefix(strings.Join(strings.Fields(query), " "), "UPDATE jobs SET state='RUNNING'") {
				for _, arg := range args {
					if arg.Value == fixture.jobID {
						hits.Add(1)
						return failedSchedulingCount{Result: result, cause: cause}, nil
					}
				}
			}
			return result, nil
		},
	})
	service, err := New(db, nil, fixture.service.now, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	claim, found, err := service.claim(t.Context())
	if !errors.Is(err, cause) || found || claim.ID != "" || hits.Load() != 1 {
		t.Fatalf("claim lost failure or exposed authority: %+v/%t/%v hits=%d", claim, found, err, hits.Load())
	}
	if state := releaseJobAuthority(t, fixture.database, fixture.jobID); state.State != "QUEUED" || state.Attempt != 0 {
		t.Fatalf("failed claim retained writes: %+v", state)
	}
}

func TestPayloadWorkerSettlesMalformedInputWithoutReclaimLoop(t *testing.T) {
	t.Parallel()
	fixture := queuedReleaseWorker(t)
	if _, err := fixture.database.ExecContext(t.Context(), `UPDATE job_input_snapshots SET input_json='{' WHERE job_id=?`,
		fixture.jobID); err != nil {
		t.Fatal(err)
	}
	did, err := fixture.service.RunOnce(t.Context())
	var syntax *json.SyntaxError
	if !did || !errors.As(err, &syntax) {
		t.Fatalf("invalid input was not claimed and settled with its parser cause: did=%t error=%v", did, err)
	}
	var state string
	var retryable bool
	err = fixture.database.QueryRowContext(t.Context(), `SELECT state,error_retryable FROM jobs WHERE id=?`, fixture.jobID).
		Scan(&state, &retryable)
	if err != nil || state != "FAILED" || retryable {
		t.Fatalf("malformed job can be reclaimed forever: %s/%t/%v", state, retryable, err)
	}
}

func TestPayloadWorkerCannotExceedItsAttemptBudget(t *testing.T) {
	t.Parallel()
	fixture := queuedReleaseWorker(t)
	if _, err := fixture.database.ExecContext(t.Context(), `UPDATE jobs SET attempt_count=max_attempts WHERE id=?`,
		fixture.jobID); err != nil {
		t.Fatal(err)
	}
	claim, found, err := fixture.service.claim(t.Context())
	if err != nil || found || claim.ID != "" {
		t.Fatalf("exhausted job received another execution: %+v/%t/%v", claim, found, err)
	}
	if after := releaseJobAuthority(t, fixture.database, fixture.jobID); after.State != "FAILED" || after.Attempt != 4 {
		t.Fatalf("exhausted job was not terminalized: %+v", after)
	}
}
