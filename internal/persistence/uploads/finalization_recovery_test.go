package uploads

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	uploadservice "retrom/internal/service/uploads"
	"retrom/internal/testsupport"
)

func TestFinalizationRecoveryPreservesOriginalDeadline(t *testing.T) {
	fixture := newFinalizationFixture(t)
	session := fixture.upload(t, []byte("bytes"))
	fixture.service.Close()
	job := fixture.complete(t, session)
	now := finalizationNow().UnixMilli()
	deadline := now + 30000
	_, err := fixture.database.ExecContext(t.Context(), `UPDATE jobs SET state='RUNNING',attempt_count=1,
worker_id='old-worker',execution_started_at_ms=?,execution_deadline_at_ms=?,leased_until_ms=? WHERE id=?`, now-570000, deadline, now-1, job)
	if err != nil {
		t.Fatal(err)
	}
	var clock atomic.Int64
	clock.Store(now)
	worker := uploadservice.New(New(fixture.database), fixture.blobs, fixture.root, func() time.Time { return time.UnixMilli(clock.Load()) })
	t.Cleanup(worker.Close)
	if err := worker.Run(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	var stored int64
	var state string
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT state,execution_deadline_at_ms FROM jobs WHERE id=?`, job).Scan(&state, &stored); err != nil {
		t.Fatal(err)
	}
	if state != "QUEUED" || stored != deadline {
		t.Fatalf("recovery reset deadline: %s %d", state, stored)
	}
	var events int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT count(*) FROM job_events WHERE job_id=? AND event_type='RETRY_SCHEDULED'`, job).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("recovery event count=%d", events)
	}
	clock.Add(1000)
	if err := worker.Run(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	var attempt int64
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT attempt_count,execution_deadline_at_ms FROM jobs WHERE id=?`, job).Scan(&attempt, &stored); err != nil {
		t.Fatal(err)
	}
	if attempt != 2 || stored != deadline {
		t.Fatalf("retry reset execution: %d %d", attempt, stored)
	}
	awaitFinalizeState(t, fixture.database, job, "SUCCEEDED")
}

func TestFinalizationDeadlinePrecedesFutureAvailability(t *testing.T) {
	fixture := newFinalizationFixture(t)
	session := fixture.upload(t, []byte("bytes"))
	fixture.service.Close()
	job := fixture.complete(t, session)
	now := finalizationNow().UnixMilli()
	_, err := fixture.database.ExecContext(t.Context(), `UPDATE jobs SET execution_started_at_ms=?,execution_deadline_at_ms=?,available_at_ms=? WHERE id=?`, now-600001, now-1, now+3600000, job)
	if err != nil {
		t.Fatal(err)
	}
	worker := uploadservice.New(New(fixture.database), fixture.blobs, fixture.root, finalizationNow)
	err = worker.Run(t.Context(), job)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired execution cause: %v", err)
	}
	awaitFinalizeState(t, fixture.database, job, "FAILED")
	var code string
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT error_code FROM jobs WHERE id=?`, job).Scan(&code); err != nil {
		t.Fatal(err)
	}
	if code != "UPLOAD_FINALIZE_TIMEOUT" {
		t.Fatalf("expired execution code=%s", code)
	}
}

func TestFinalizationRecoveryEventFailureRollsBackRequeue(t *testing.T) {
	fixture := newFinalizationFixture(t)
	session := fixture.upload(t, []byte("bytes"))
	fixture.service.Close()
	job := fixture.complete(t, session)
	now := finalizationNow().UnixMilli()
	if _, err := fixture.database.ExecContext(t.Context(), `UPDATE jobs SET state='RUNNING',attempt_count=1,
worker_id='old-worker',execution_started_at_ms=?,execution_deadline_at_ms=?,leased_until_ms=? WHERE id=?`,
		now-100000, now+500000, now-1, job); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("recovery event write unavailable")
	hits := 0
	database := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
		if strings.Contains(query, "INSERT INTO job_events") && len(args) == 5 && args[0].Value == job && args[2].Value == "RETRY_SCHEDULED" {
			hits++
			return cause
		}
		return nil
	}})
	err := uploadservice.New(New(database), fixture.blobs, fixture.root, finalizationNow).Run(t.Context(), job)
	if !errors.Is(err, cause) || hits != 1 {
		t.Fatalf("recovery cause/hits: %v %d", err, hits)
	}
	var state, owner string
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT state,worker_id FROM jobs WHERE id=?`, job).Scan(&state, &owner); err != nil {
		t.Fatal(err)
	}
	if state != "RUNNING" || owner != "old-worker" {
		t.Fatalf("partial recovery committed: %s/%s", state, owner)
	}
}
