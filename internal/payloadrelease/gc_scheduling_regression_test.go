package payloadrelease

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/testsupport"
)

type gcSchedulingFixture struct {
	database *sql.DB
	blobs    *blobstore.Store
	service  *Service
}

func gcBoundArgument(args []driver.NamedValue, expected string) bool {
	for _, arg := range args {
		if arg.Value == expected {
			return true
		}
	}
	return false
}

func newGCSchedulingFixture(t *testing.T) gcSchedulingFixture {
	t.Helper()
	db := schedulingGame(t)
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := blobs.Put(bytes.NewBufferString("GC scheduling reference"))
	if err != nil {
		t.Fatal(err)
	}
	seedManualGC(t, db, metadata)
	service, err := New(db, blobs, func() time.Time { return time.UnixMilli(10) }, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	return gcSchedulingFixture{database: db, blobs: blobs, service: service}
}

func TestGCSchedulingRollsBackWhenJobRowCountCannotBeConfirmed(t *testing.T) {
	t.Parallel()
	fixture := newGCSchedulingFixture(t)
	cause := errors.New("GC job affected-row failure")
	var hits atomic.Int64
	fault := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.HasPrefix(strings.Join(strings.Fields(query), " "), "INSERT INTO jobs") {
				for _, arg := range args {
					if arg.Value == "manual-gc-blob" {
						hits.Add(1)
						return failedSchedulingCount{Result: result, cause: cause}, nil
					}
				}
			}
			return result, nil
		},
	})
	service, err := New(fault, fixture.blobs, fixture.service.now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	err = service.stageAllUnreferenced(t.Context())
	var jobs, candidates int
	readErr := fixture.database.QueryRowContext(t.Context(), `SELECT
 (SELECT count(*) FROM jobs WHERE scope_type='BLOB' AND scope_id='manual-gc-blob'),
 (SELECT count(*) FROM blob_gc_candidates WHERE blob_id='manual-gc-blob')`).Scan(&jobs, &candidates)
	if !errors.Is(err, cause) || hits.Load() != 1 || readErr != nil || jobs != 0 || candidates != 0 {
		t.Fatalf("GC schedule retained unconfirmed writes: jobs=%d candidates=%d hits=%d error=%v read=%v",
			jobs, candidates, hits.Load(), err, readErr)
	}
}

func TestImmediateGCRollsBackWhenAdvanceRowCountCannotBeConfirmed(t *testing.T) {
	t.Parallel()
	fixture := newGCSchedulingFixture(t)
	if err := fixture.service.stageAllUnreferenced(t.Context()); err != nil {
		t.Fatal(err)
	}
	var jobID string
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT gc_job_id FROM blob_gc_candidates WHERE blob_id='manual-gc-blob'`).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("GC advance affected-row failure")
	var hits atomic.Int64
	fault := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.HasPrefix(strings.Join(strings.Fields(query), " "), "UPDATE jobs SET available_at_ms=") && gcBoundArgument(args, jobID) {
				hits.Add(1)
				return failedSchedulingCount{Result: result, cause: cause}, nil
			}
			return result, nil
		},
	})
	service, err := New(fault, fixture.blobs, fixture.service.now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	result, err := service.ScheduleImmediateGC(t.Context(), "manual-gc-user")
	var scheduled, available, audits int64
	readErr := fixture.database.QueryRowContext(t.Context(), `SELECT candidate.scheduled_at_ms,job.available_at_ms,
 (SELECT count(*) FROM audit_events WHERE action='STORAGE_CLEANUP_REQUESTED')
 FROM blob_gc_candidates candidate JOIN jobs job ON job.id=candidate.gc_job_id
 WHERE candidate.blob_id='manual-gc-blob'`).Scan(&scheduled, &available, &audits)
	expected := int64(10) + (24 * time.Hour).Milliseconds()
	if !errors.Is(err, cause) || hits.Load() != 1 || result != (ImmediateGCResult{}) || readErr != nil ||
		scheduled != expected || available != expected || audits != 0 {
		t.Fatalf("GC advance retained partial success: result=%+v scheduled=%d available=%d audits=%d hits=%d error=%v read=%v",
			result, scheduled, available, audits, hits.Load(), err, readErr)
	}
}
