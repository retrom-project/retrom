package jobs

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/foundation/cleanup"
	jobsmodel "retrom/internal/model/jobs"
	"retrom/internal/repo/store"
)

func TestCancellationEventRollsBackWithOuterFailure(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_786_000_000_000)
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "jobs.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	insertJob(t, database, "job", "MEDIA_FETCH", "QUEUED", nil, now.UnixMilli())
	failed := errors.New("dependent operation failed")
	err = New(database.SQL).WithWrite(t.Context(), func(records jobsmodel.Records) error {
		if err := records.Cancel(t.Context(), jobsmodel.Cancellation{
			JobID: "job", ExpectedVersion: 1, State: "CANCELLED",
			AtMS: now.UnixMilli(), FinishedAtMS: timePointer(now.UnixMilli()), Reason: "stop", Event: []byte(`{"reason":"stop"}`),
		}); err != nil {
			return err
		}
		return failed
	})
	if !errors.Is(err, failed) {
		t.Fatal(err)
	}
	var state string
	var version, events int
	err = database.SQL.QueryRowContext(t.Context(), `SELECT state,version,(SELECT count(*) FROM job_events WHERE job_id='job') FROM jobs WHERE id='job'`).Scan(&state, &version, &events)
	if err != nil || state != "QUEUED" || version != 1 || events != 0 {
		t.Fatalf("state=%s version=%d events=%d error=%v", state, version, events, err)
	}
}

func TestRetrySnapshotRollsBackOnVersionConflict(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_786_000_000_000)
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "jobs.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	insertJob(t, database, "job", "MEDIA_FETCH", "FAILED", int64(1), now.UnixMilli())
	err = New(database.SQL).WithWrite(t.Context(), func(records jobsmodel.Records) error {
		return records.Retry(t.Context(), jobsmodel.RetryWrite{
			JobID: "job", ExpectedVersion: 8, ExecutionNo: 2,
			AtMS: now.UnixMilli(), Input: []byte(`{}`), InputDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Payload: []byte(`{"schemaVersion":1,"inputExecutionNo":2}`), Event: []byte(`{"executionNo":2}`),
		})
	})
	if !errors.Is(err, jobsmodel.ErrConflict) {
		t.Fatal(err)
	}
	var snapshots, version int
	err = database.SQL.QueryRowContext(context.Background(), `SELECT version,(SELECT count(*) FROM job_input_snapshots WHERE job_id='job') FROM jobs WHERE id='job'`).Scan(&version, &snapshots)
	if err != nil || version != 1 || snapshots != 1 {
		t.Fatalf("version=%d snapshots=%d error=%v", version, snapshots, err)
	}
}

func timePointer(value int64) *int64 { return &value }
