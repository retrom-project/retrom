package payloadrelease

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"retrom/internal/testsupport"
)

func setGCTestState(t *testing.T, database *sql.DB, id, state string) {
	t.Helper()
	var finished any
	if state == "FAILED" {
		finished = int64(10)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE jobs SET state=?,finished_at_ms=? WHERE id=?`, state, finished, id); err != nil {
		t.Fatal(err)
	}
}

func TestGCReferenceRestorationCancelsOnlyQueuedExecution(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"QUEUED", "RUNNING", "FAILED"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			fixture := newGCSchedulingFixture(t)
			id := stageGCTestCandidate(t, fixture)
			setGCTestState(t, fixture.database, id, state)
			before := releaseJobAuthority(t, fixture.database, id)
			_, err := fixture.database.ExecContext(t.Context(), `INSERT INTO game_assets
 (id,game_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms)
 VALUES('gc-protection','schedule-game','manual-gc-blob','COVER',0,1,1,'image/png',1)`)
			if err != nil {
				t.Fatal(err)
			}
			if err := fixture.service.stageAllUnreferenced(t.Context()); err != nil {
				t.Fatal(err)
			}
			after := releaseJobAuthority(t, fixture.database, id)
			if state == "QUEUED" {
				if after.State != "SUCCEEDED" || after.Version != before.Version+1 {
					t.Fatalf("protected queued job not closed: %+v", after)
				}
			} else if before != after {
				t.Fatalf("restored reference changed owned or failed job: %+v/%+v", before, after)
			}
			var candidates, blobs, events int
			err = fixture.database.QueryRowContext(t.Context(), `SELECT
 (SELECT count(*) FROM blob_gc_candidates WHERE blob_id='manual-gc-blob'),
 (SELECT count(*) FROM blobs WHERE id='manual-gc-blob'),
 (SELECT count(*) FROM job_events WHERE job_id=? AND event_type='SUCCEEDED')`, id).Scan(&candidates, &blobs, &events)
			expectedEvents := 0
			if state == "QUEUED" {
				expectedEvents = 1
			}
			if err != nil || candidates != 0 || blobs != 1 || events != expectedEvents {
				t.Fatalf("protection result: candidates=%d blobs=%d events=%d error=%v", candidates, blobs, events, err)
			}
		})
	}
}

func TestGCImmediateCannotPublishAfterActualCommitCancellation(t *testing.T) {
	t.Parallel()
	fixture := newGCSchedulingFixture(t)
	id := stageGCTestCandidate(t, fixture)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var hits atomic.Int64
	fault := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.HasPrefix(strings.Join(strings.Fields(query), " "), "INSERT INTO audit_events") &&
				gcBoundArgument(args, "manual-gc-user") {
				hits.Add(1)
				cancel()
			}
			return result, nil
		},
	})
	result, err := gcFaultService(t, fixture, fault).ScheduleImmediateGC(ctx, "manual-gc-user")
	if (!errors.Is(err, context.Canceled) && !errors.Is(err, sql.ErrTxDone)) || result != (ImmediateGCResult{}) || hits.Load() != 1 {
		t.Fatalf("cancelled commit published: %+v/%v hits=%d", result, err, hits.Load())
	}
	assertGCImmediateRollback(t, fixture.database, id)
}
