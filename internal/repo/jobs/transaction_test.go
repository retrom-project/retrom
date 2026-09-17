package jobs

import (
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/model/jobs"
	"retrom/internal/repo/store"
)

func TestCancellationRollsBackOnVersionConflict(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_786_000_000_000)
	database, err := store.Open(
		t.Context(), filepath.Join(t.TempDir(), "jobs.db"),
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	insertJob(t, database, "job", "MEDIA_FETCH", "QUEUED", nil, now.UnixMilli())
	_, err = New(database.SQL).CommitCancel(t.Context(), jobs.CancelCommand{
		JobID: "job", ExpectedVersion: 99, Reason: "stop",
		NowMS: now.UnixMilli(),
	})
	if err == nil {
		t.Fatal("expected conflict error")
	}
	var state string
	var version, events int
	err = database.SQL.QueryRowContext(
		t.Context(),
		`SELECT state,version,(SELECT count(*) FROM job_events WHERE job_id='job')
		 FROM jobs WHERE id='job'`,
	).Scan(&state, &version, &events)
	if err != nil || state != "QUEUED" || version != 1 || events != 0 {
		t.Fatalf("state=%s version=%d events=%d error=%v", state, version, events, err)
	}
}

func TestRetryRollsBackOnVersionConflict(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_786_000_000_000)
	database, err := store.Open(
		t.Context(), filepath.Join(t.TempDir(), "jobs.db"),
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	insertJob(t, database, "job", "MEDIA_FETCH", "FAILED", int64(1), now.UnixMilli())
	_, err = New(database.SQL).CommitRetry(t.Context(), jobs.RetryCommand{
		JobID: "job", ExecutionID: "01980000-0000-7000-8000-000000000099",
		ExpectedVersion: 8, NowMS: now.UnixMilli(),
	})
	if err == nil {
		t.Fatal("expected conflict or eligibility error")
	}
	var snapshots, version int
	err = database.SQL.QueryRowContext(
		t.Context(),
		`SELECT version,(SELECT count(*) FROM job_input_snapshots WHERE job_id='job')
		 FROM jobs WHERE id='job'`,
	).Scan(&version, &snapshots)
	if err != nil || version != 1 || snapshots != 1 {
		t.Fatalf("version=%d snapshots=%d error=%v", version, snapshots, err)
	}
}
