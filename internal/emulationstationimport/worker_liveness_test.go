package emulationstationimport

import (
	"context"
	"testing"
	"time"

	"retrom/internal/testassert"
)

func TestImportStopsAndRetriesWhenItemCannotLeaveWorkingState(t *testing.T) {
	fixture := newLifecycleFixture(t)
	started, unit := startLifecycleImport(t, fixture, "", "nes")
	mustExecEmulationStationTest(t, fixture.database, `
CREATE TRIGGER test_reject_emulationstation_working_state_exit
BEFORE UPDATE OF execution_state ON emulationstation_import_items
WHEN OLD.execution_state='COPYING' AND NEW.execution_state<>OLD.execution_state
BEGIN SELECT RAISE(ABORT,'injected terminalization failure'); END;
`)

	ctx, cancel := context.WithCancel(fixture.context)
	done := make(chan struct{})
	go func() {
		fixture.service.execute(ctx, unit)
		close(done)
	}()
	select {
	case <-done:
		cancel()
	case <-time.After(500 * time.Millisecond):
		cancel()
		<-done
		t.Fatal("import worker repeated an item after its terminalization failed")
	}

	var jobState, itemState string
	var retryEvents int64
	testassert.False(t, fixture.database.QueryRowContext(fixture.context, `
SELECT
 (SELECT state FROM jobs WHERE id=?),
 (SELECT execution_state FROM emulationstation_import_items WHERE import_id=? ORDER BY id LIMIT 1),
 (SELECT count(*) FROM job_events WHERE job_id=? AND event_type='RETRY_SCHEDULED')
`, unit.JobID, started.ID, unit.JobID).Scan(&jobState, &itemState, &retryEvents) != nil,
		"read liveness failure outcome")
	testassert.Falsef(t, jobState != "QUEUED" || itemState != "COPYING" || retryEvents != 1,
		"liveness outcome = job:%s item:%s retries:%d", jobState, itemState, retryEvents)
}
