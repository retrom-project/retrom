package jobs

import (
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/repo/store"
	jobservice "retrom/internal/service/jobs"
)

func TestProgressReadsKeepStateAndEventsInOneSnapshot(t *testing.T) {
	now := time.UnixMilli(1_786_000_000_000)
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "jobs.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	insertJob(t, database, "job", "MEDIA_FETCH", "RUNNING", nil, now.UnixMilli())
	repository := New(database.ReadOnly)
	err = repository.WithRead(t.Context(), func(records jobservice.ReadRecords) error {
		first, err := records.Detail(t.Context(), "job")
		if err != nil {
			return err
		}
		commitJobCompletion(t, database, now.UnixMilli())
		second, err := records.Detail(t.Context(), "job")
		if err != nil {
			return err
		}
		maximum, err := records.EventMaximum(t.Context())
		if err != nil {
			return err
		}
		events, err := records.JobEvents(t.Context(), jobservice.EventQuery{ResourceID: "job", Limit: 1000})
		if err != nil || first.State != "RUNNING" || second.State != first.State || maximum != 0 || len(events) != 0 {
			t.Fatalf("mixed snapshots: before=%s after=%s maximum=%d events=%d error=%v",
				first.State, second.State, maximum, len(events), err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	batch, err := jobservice.New(repository, time.Now).JobEvents(t.Context(), "job", 0)
	if err != nil || !batch.Terminal || len(batch.Events) != 1 || batch.Events[0].Type != "SUCCEEDED" {
		t.Fatalf("next snapshot missed completion: %+v error=%v", batch, err)
	}
}

func commitJobCompletion(t *testing.T, database *store.DB, now int64) {
	t.Helper()
	transaction, err := database.SQL.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(t.Context(), `
UPDATE jobs SET state='SUCCEEDED',finished_at_ms=? WHERE id='job'`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(t.Context(), `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,scope_type,scope_id,'SUCCEEDED','{}',? FROM jobs WHERE id='job'`, now); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
}
