package payloadrelease

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"retrom/internal/repo/dbexec"
	repository "retrom/internal/repo/payloadrelease"
	application "retrom/internal/service/payloadrelease"
	"retrom/internal/testkit/testsupport"
)

func TestGCScheduleAtomicallyPersistsAllDurableEvidence(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"jobs", "job_input_snapshots", "job_events", "blob_gc_candidates"} {
		for _, failure := range []string{"sql", "count", "zero"} {
			t.Run(stage+"/"+failure, func(t *testing.T) {
				t.Parallel()
				fixture := newGCSchedulingFixture(t)
				cause := errors.New("GC durable evidence failure")
				var hits atomic.Int64
				hooks := gcMutationFault("INSERT INTO "+stage, "", failure, cause, &hits)
				fault := testsupport.OpenSQLFaultDatabase(t, fixture.database, hooks)
				service := gcFaultService(t, fixture, fault)
				err := service.stageAllUnreferenced(t.Context())
				expected := cause
				if failure == "zero" {
					expected = application.ErrGCSnapshotChanged
				}
				if !errors.Is(err, expected) || hits.Load() != 1 {
					t.Fatalf("GC evidence error: %v hits=%d", err, hits.Load())
				}
				assertNoGCSchedule(t, fixture.database)
			})
		}
	}
}

func gcFaultService(t *testing.T, fixture gcSchedulingFixture, database *sql.DB) *Service {
	t.Helper()
	service, err := New(database, fixture.blobs, fixture.service.now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	return service
}

func gcMutationFault(prefix, identity, failure string, cause error, hits *atomic.Int64) testsupport.SQLFaultHooks {
	return testsupport.SQLFaultHooks{AfterExec: func(_ context.Context, query string, args []driver.NamedValue,
		result driver.Result,
	) (driver.Result, error) {
		q := strings.Join(strings.Fields(query), " ")
		matched := strings.HasPrefix(q, prefix) && (identity == "" || gcBoundArgument(args, identity))
		if identity == "" {
			// Input rows bind the generated ID; match the immutable input's exact scope.
			matched = matched && gcEvidenceIdentity(prefix, args)
		}
		if !matched {
			return result, nil
		}
		hits.Add(1)
		switch failure {
		case "count":
			return failedSchedulingCount{Result: result, cause: cause}, nil
		case "zero":
			return driver.RowsAffected(0), nil
		default:
			return result, cause
		}
	}}
}

func gcEvidenceIdentity(prefix string, args []driver.NamedValue) bool {
	if prefix != "INSERT INTO job_input_snapshots" {
		return gcBoundArgument(args, "manual-gc-blob")
	}
	for _, arg := range args {
		value, ok := arg.Value.(string)
		if ok && strings.Contains(value, `"id":"manual-gc-blob"`) && strings.Contains(value, `"kind":"BLOB_GC"`) {
			return true
		}
	}
	return false
}

func assertNoGCSchedule(t *testing.T, database *sql.DB) {
	t.Helper()
	var jobs, inputs, events, candidates int
	err := database.QueryRowContext(t.Context(), `SELECT
 (SELECT count(*) FROM jobs WHERE kind='BLOB_GC'),
 (SELECT count(*) FROM job_input_snapshots input JOIN jobs job ON job.id=input.job_id WHERE job.kind='BLOB_GC'),
 (SELECT count(*) FROM job_events WHERE scope_type='BLOB'),
 (SELECT count(*) FROM blob_gc_candidates)`).Scan(&jobs, &inputs, &events, &candidates)
	if err != nil || jobs != 0 || inputs != 0 || events != 0 || candidates != 0 {
		t.Fatalf("partial GC schedule: jobs=%d inputs=%d events=%d candidates=%d error=%v", jobs, inputs, events, candidates, err)
	}
}

func TestGCImmediateAuditFailureRollsBackCandidateAndJob(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"QUEUED", "RUNNING", "FAILED"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			fixture := newGCSchedulingFixture(t)
			id := stageGCTestCandidate(t, fixture)
			setGCTestState(t, fixture.database, id, state)
			before := releaseJobAuthority(t, fixture.database, id)
			cause := errors.New("GC audit affected-row failure")
			var hits atomic.Int64
			fault := testsupport.OpenSQLFaultDatabase(t, fixture.database,
				gcMutationFault("INSERT INTO audit_events", "manual-gc-user", "count", cause, &hits))
			result, err := gcFaultService(t, fixture, fault).ScheduleImmediateGC(t.Context(), "manual-gc-user")
			if !errors.Is(err, cause) || result != (ImmediateGCResult{}) || hits.Load() != 1 {
				t.Fatalf("audit failure escaped: result=%+v error=%v hits=%d", result, err, hits.Load())
			}
			if after := releaseJobAuthority(t, fixture.database, id); after != before {
				t.Fatalf("audit failure changed job: before=%+v after=%+v", before, after)
			}
			assertGCImmediateRollback(t, fixture.database, id)
		})
	}
}

func stageGCTestCandidate(t *testing.T, fixture gcSchedulingFixture) string {
	t.Helper()
	if err := fixture.service.stageAllUnreferenced(t.Context()); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := fixture.database.QueryRowContext(t.Context(),
		`SELECT gc_job_id FROM blob_gc_candidates WHERE blob_id='manual-gc-blob'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func assertGCImmediateRollback(t *testing.T, database *sql.DB, id string) {
	t.Helper()
	var due, inputs, events, audits int64
	err := database.QueryRowContext(t.Context(), `SELECT candidate.scheduled_at_ms,
 (SELECT count(*) FROM job_input_snapshots WHERE job_id=?),
 (SELECT count(*) FROM job_events WHERE job_id=? AND event_type='MANUAL_RETRY'),
 (SELECT count(*) FROM audit_events WHERE action='STORAGE_CLEANUP_REQUESTED')
 FROM blob_gc_candidates candidate WHERE candidate.gc_job_id=?`, id, id, id).Scan(&due, &inputs, &events, &audits)
	if err != nil || due != 10+(24*time.Hour).Milliseconds() || inputs != 1 || events != 0 || audits != 0 {
		t.Fatalf("partial GC advance: due=%d inputs=%d events=%d audit=%d err=%v", due, inputs, events, audits, err)
	}
}

func TestGCFenceRejectsChangedBlobOrProtectionBeforeQueueing(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"blob", "protection", "job", "input", "candidate"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			fixture := newGCSchedulingFixture(t)
			stageGCTestCandidate(t, fixture)
			tx, err := fixture.database.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer dbexec.Rollback(tx)
			scope := repository.BindGC(tx)
			facts, err := scope.Read.Selected(t.Context(), []string{"manual-gc-blob"})
			if err != nil || len(facts) != 1 {
				t.Fatalf("GC facts: %+v/%v", facts, err)
			}
			mutateGCFacts(t, tx, change, facts[0].Candidate.Work.ID)
			err = scope.Write.Fence(t.Context(), facts)
			if !errors.Is(err, application.ErrGCSnapshotChanged) {
				t.Errorf("changed %s accepted: %v", change, err)
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			assertGCImmediateRollback(t, fixture.database, facts[0].Candidate.Work.ID)
		})
	}
}

func mutateGCFacts(t *testing.T, tx *sql.Tx, change, jobID string) {
	t.Helper()
	queries := map[string]string{
		"blob": `UPDATE blobs SET size_bytes=size_bytes+1 WHERE id='manual-gc-blob'`,
		"protection": `INSERT INTO game_assets(id,game_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms)
 VALUES('gc-protection','schedule-game','manual-gc-blob','COVER',0,1,1,'image/png',1)`,
		"job":       `UPDATE jobs SET version=version+1 WHERE id=?`,
		"input":     `UPDATE job_input_snapshots SET input_json='{}' WHERE job_id=?`,
		"candidate": `UPDATE blob_gc_candidates SET scheduled_at_ms=scheduled_at_ms+1 WHERE gc_job_id=?`,
	}
	var args []any
	if change != "blob" && change != "protection" {
		args = []any{jobID}
	}
	if _, err := tx.ExecContext(t.Context(), queries[change], args...); err != nil {
		t.Fatal(err)
	}
}
