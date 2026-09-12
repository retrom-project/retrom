package emulationstationimport

import (
	"fmt"
	"testing"
	"time"
)

func TestESFailureCannotWriteReplacementAttempt(t *testing.T) {
	for _, retryable := range []bool{false, true} {
		t.Run(fmt.Sprint(retryable), func(t *testing.T) {
			fixture, _, unit, _ := nextScannedOwnedResult(t)
			mustExecEmulationStationTest(t, fixture.database, `UPDATE jobs
SET attempt_count=attempt_count+1,worker_id='replacement-worker',version=version+1 WHERE id=?`, unit.JobID)
			before := executionAuthorityState(t, fixture, unit)
			fixture.service.fail(fixture.context, unit, "INTERNAL_ERROR", retryable)
			if after := executionAuthorityState(t, fixture, unit); after != before {
				t.Fatalf("old worker changed replacement: before=%s after=%s", before, after)
			}
		})
	}
}

func TestESLiveCancellationRequiresUnexpiredOwner(t *testing.T) {
	for _, mutation := range []string{"owner", "lease", "deadline"} {
		t.Run(mutation, func(t *testing.T) {
			fixture := newLifecycleFixture(t)
			started, unit := startLifecycleImport(t, fixture, "", "nes")
			requestExecutionCancellation(t, fixture, started.ID)
			switch mutation {
			case "owner":
				mustExecEmulationStationTest(t, fixture.database,
					`UPDATE jobs SET worker_id='replacement-worker',version=version+1 WHERE id=?`, unit.JobID)
			case "lease":
				*fixture.now = fixture.now.Add(time.Minute)
			case "deadline":
				*fixture.now = time.UnixMilli(unit.DeadlineAtMS)
			}
			before := executionAuthorityState(t, fixture, unit)
			closed, err := fixture.service.closeCancelled(fixture.context, unit)
			if after := executionAuthorityState(t, fixture, unit); closed || after != before {
				t.Fatalf("invalid owner closed=%v error=%v before=%s after=%s", closed, err, before, after)
			}
		})
	}
}

func TestESLiveCancellationClearsPartialScan(t *testing.T) {
	fixture, created, unit, result := nextScannedOwnedResult(t)
	if err := fixture.service.persistScanHeaders(fixture.context, unit, result); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.persistScanItems(fixture.context, unit, result.Items[:1]); err != nil {
		t.Fatal(err)
	}
	requestExecutionCancellation(t, fixture, created.ID)
	closed, err := fixture.service.closeCancelled(fixture.context, unit)
	if err != nil || !closed {
		t.Fatalf("close partial scan=%v error=%v", closed, err)
	}
	var state string
	var games, staged, payload, events int64
	err = fixture.database.QueryRowContext(fixture.context, `SELECT plan.state,plan.game_count,
(SELECT count(*) FROM emulationstation_import_items WHERE import_id=plan.id),
(SELECT count(*) FROM jobs WHERE scope_type='EMULATIONSTATION_IMPORT_ITEM'),
(SELECT count(*) FROM job_events WHERE job_id=plan.scan_job_id AND event_type='CANCELLED')
FROM emulationstation_imports plan WHERE id=?`, created.ID).Scan(&state, &games, &staged, &payload, &events)
	if err != nil || state != "CANCELLED" || games != 0 || staged != 0 || payload != 0 || events != 1 {
		t.Fatalf("cancelled partial scan=%s games=%d staged=%d payload=%d events=%d error=%v",
			state, games, staged, payload, events, err)
	}
}

func requestExecutionCancellation(t *testing.T, fixture lifecycleFixture, id string) {
	t.Helper()
	current, err := fixture.service.Get(fixture.context, id)
	if err != nil {
		t.Fatal(err)
	}
	_, pending, err := fixture.service.Cancel(fixture.context, id, current.Version, "Stop running execution", fixture.userID)
	if err != nil || !pending {
		t.Fatalf("request cancellation=%v error=%v", pending, err)
	}
}

func executionAuthorityState(t *testing.T, fixture lifecycleFixture, unit work) string {
	t.Helper()
	var result string
	err := fixture.database.QueryRowContext(fixture.context, `SELECT json_array(job.state,job.version,
job.worker_id,job.attempt_count,job.execution_deadline_at_ms,job.leased_until_ms,job.error_code,
plan.state,plan.version,plan.failed_item_count,plan.cancelled_item_count,
(SELECT count(*) FROM job_events WHERE job_id=job.id),
(SELECT json_group_array(json_array(id,execution_state,version)) FROM emulationstation_import_items WHERE import_id=plan.id))
FROM jobs job JOIN emulationstation_imports plan ON plan.id=job.scope_id WHERE job.id=?`, unit.JobID).Scan(&result)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
