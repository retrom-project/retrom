package metadatascrape

import (
	"context"
	"errors"
	"testing"
	"time"

	"retrom/internal/service/metadatascrape"
)

func TestMetadataRecoveryFinalizesExpiredOrExhaustedQueuedExecution(t *testing.T) {
	for _, test := range []struct {
		name     string
		attempts int
		deadline int64
		cause    error
		code     string
	}{
		{"deadline", 1, recoveryTime.UnixMilli(), context.DeadlineExceeded, "METADATA_EXECUTION_EXPIRED"},
		{"attempts", 4, recoveryTime.Add(time.Hour).UnixMilli(), metadatascrape.ErrAttemptsExhausted, "METADATA_ATTEMPTS_EXHAUSTED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := recoveryDatabase(t)
			recoveryExec(t, database, `UPDATE jobs SET attempt_count=?,execution_started_at_ms=?,execution_deadline_at_ms=? WHERE id='job'`, test.attempts, recoveryTime.Add(-time.Hour).UnixMilli(), test.deadline)
			processor := recoveryProcess(func(context.Context, metadatascrape.WorkerClaim, string) (int, string, error) {
				t.Fatal("terminal execution processed")
				return 0, "", nil
			})
			err := metadatascrape.NewWorker(NewWorker(database), processor, recoveryNow).Run(t.Context(), "run")
			if !errors.Is(err, test.cause) {
				t.Fatalf("terminal cause=%v", err)
			}
			var state, run, code string
			var attempts, events int
			err = database.QueryRowContext(t.Context(), `SELECT j.state,r.state,j.error_code,j.attempt_count,
 (SELECT count(*) FROM job_events WHERE job_id=j.id AND event_type='FAILED')
 FROM jobs j JOIN metadata_scrape_runs r ON r.job_id=j.id WHERE j.id='job'`).Scan(&state, &run, &code, &attempts, &events)
			if err != nil {
				t.Fatal(err)
			}
			if state != "FAILED" || run != "FAILED" || code != test.code || attempts != test.attempts || events != 1 {
				t.Fatalf("terminal=(%s,%s,%s,%d,%d)", state, run, code, attempts, events)
			}
		})
	}
}

func TestMetadataRecoveryCancelsExpiredRequestedExecution(t *testing.T) {
	database := recoveryDatabase(t)
	now := recoveryTime.UnixMilli()
	recoveryExec(t, database, `UPDATE jobs SET state='CANCEL_REQUESTED',attempt_count=1,worker_id='old',
 execution_started_at_ms=?,execution_deadline_at_ms=?,leased_until_ms=?,cancel_requested_at_ms=?,cancel_reason='stop'
 WHERE id='job'`, now-60001, now+1000, now-1, now)
	processor := recoveryProcess(func(context.Context, metadatascrape.WorkerClaim, string) (int, string, error) {
		t.Fatal("cancelled execution processed")
		return 0, "", nil
	})
	_ = metadatascrape.NewWorker(NewWorker(database), processor, recoveryNow).Run(t.Context(), "run")
	var state, run string
	if err := database.QueryRowContext(t.Context(), `SELECT j.state,r.state FROM jobs j JOIN metadata_scrape_runs r ON r.job_id=j.id WHERE j.id='job'`).Scan(&state, &run); err != nil {
		t.Fatal(err)
	}
	if state != "CANCELLED" || run != "CANCELLED" {
		t.Fatalf("cancelled=%s/%s", state, run)
	}
}

func TestMetadataRecoveryOnlyDispatchesAvailableWork(t *testing.T) {
	database := recoveryDatabase(t)
	repository := NewWorker(database)
	ids, err := repository.Recoverable(t.Context(), recoveryTime.UnixMilli())
	if err != nil || len(ids) != 1 || ids[0] != "run" {
		t.Fatalf("queued=%v/%v", ids, err)
	}
	recoveryExec(t, database, `UPDATE jobs SET available_at_ms=? WHERE id='job'`, recoveryTime.Add(time.Minute).UnixMilli())
	ids, err = repository.Recoverable(t.Context(), recoveryTime.UnixMilli())
	if err != nil || len(ids) != 0 {
		t.Fatalf("future queued=%v/%v", ids, err)
	}
}

func TestExpiredMetadataDeadlineIsSettledBeforeFutureAvailability(t *testing.T) {
	database := recoveryDatabase(t)
	now := recoveryTime.UnixMilli()
	recoveryExec(t, database, `UPDATE jobs SET attempt_count=1,execution_started_at_ms=?,execution_deadline_at_ms=?,available_at_ms=? WHERE id='job'`, now-3600000, now, now+60000)
	repository := NewWorker(database)
	ids, err := repository.Recoverable(t.Context(), now)
	if err != nil || len(ids) != 1 {
		t.Fatalf("expired queued execution hidden until availability: %v/%v", ids, err)
	}
	err = metadatascrape.NewWorker(repository, nil, recoveryNow).Run(t.Context(), "run")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired delayed execution=%v", err)
	}
}

func TestMetadataHardDeadlineExpiresBeforeLease(t *testing.T) {
	database := recoveryDatabase(t)
	now := recoveryTime.UnixMilli()
	recoveryExec(t, database, `UPDATE jobs SET state='RUNNING',attempt_count=1,worker_id='old-worker',
 execution_started_at_ms=?,execution_deadline_at_ms=?,leased_until_ms=? WHERE id='job'`, now-3600000, now, now+60000)
	repository := NewWorker(database)
	ids, err := repository.Recoverable(t.Context(), now)
	if err != nil || len(ids) != 1 {
		t.Fatalf("hard expiry waited for lease: %v/%v", ids, err)
	}
	err = metadatascrape.NewWorker(repository, nil, recoveryNow).Run(t.Context(), "run")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hard expiry=%v", err)
	}
}
