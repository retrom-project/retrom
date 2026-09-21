package emulationstationimport

import (
	"errors"
	"testing"

	"retrom/internal/testassert"
)

func TestRetryRevalidatesFrozenInputs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, lifecycleFixture, Summary)
		want   error
	}{
		{
			name: "root configuration",
			mutate: func(_ *testing.T, fixture lifecycleFixture, _ Summary) {
				root := fixture.service.roots["games"]
				root.digest = "changed-root-configuration"
				fixture.service.roots["games"] = root
			},
			want: ErrSourceChanged,
		},
		{
			name: "gamelist snapshot",
			mutate: func(t *testing.T, fixture lifecycleFixture, _ Summary) {
				writeScanFile(t, fixture.source, "gamelist.xml", []byte(
					`<gameList><game><path>./publish.nes</path><name>Changed</name></game></gameList>`,
				))
			},
			want: ErrSourceChanged,
		},
		{
			name: "mapping target",
			mutate: func(t *testing.T, fixture lifecycleFixture, summary Summary) {
				mustExecEmulationStationTest(t, fixture.database, `
UPDATE platform_instances
SET version=version+1,updated_at_ms=updated_at_ms+1
WHERE id=(
 SELECT target_platform_instance_id FROM emulationstation_import_collections
 WHERE import_id=? AND mapping_action='IMPORT' LIMIT 1
)`, summary.ID)
			},
			want: ErrMappingTargetChanged,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newLifecycleFixture(t)
			started, unit := startLifecycleImport(t, fixture, "", "nes")
			failed := markClaimedImportRetryable(t, fixture, started, unit)
			test.mutate(t, fixture, failed)

			_, err := fixture.service.Retry(fixture.context, failed.ID, failed.Version, fixture.userID)
			testassert.Truef(t, errors.Is(err, test.want), "retry error = %v, want %v", err, test.want)
			assertRetryWasNotQueued(t, fixture, failed)
		})
	}
}

func TestRetryRejectsAggregateWithoutRetryableItems(t *testing.T) {
	fixture := newLifecycleFixture(t)
	started, unit := startLifecycleImport(t, fixture, "", "nes")
	failed := markClaimedImportRetryable(t, fixture, started, unit)
	mustExecEmulationStationTest(t, fixture.database, `
UPDATE emulationstation_import_items SET retryable=0 WHERE import_id=?`, failed.ID)

	_, err := fixture.service.Retry(fixture.context, failed.ID, failed.Version, fixture.userID)
	testassert.Truef(t, errors.Is(err, ErrNotRetryable), "retry error = %v", err)
	assertRetryWasNotQueued(t, fixture, failed)
}

func TestSourceChangedFailureDoesNotMakeAggregateRetryable(t *testing.T) {
	fixture := newLifecycleFixture(t)
	started, unit := startLifecycleImport(t, fixture, "", "nes")
	writeScanFile(t, fixture.source, "publish.nes", []byte("changed after frozen scan"))
	fixture.service.execute(fixture.context, unit)

	finished, err := fixture.service.Get(fixture.context, started.ID)
	testassert.Falsef(t, testassert.Any(
		func() bool { return err != nil },
		func() bool { return finished.State != "PARTIAL_FAILURE" },
		func() bool { return finished.Counts.Failed != 1 },
		func() bool { return finished.Retryable },
	), "finished summary = %#v, error = %v", finished, err)
	_, err = fixture.service.Retry(fixture.context, finished.ID, finished.Version, fixture.userID)
	testassert.Truef(t, errors.Is(err, ErrNotRetryable), "retry error = %v", err)
}

func markClaimedImportRetryable(
	t *testing.T,
	fixture lifecycleFixture,
	started Summary,
	unit work,
) Summary {
	t.Helper()
	now := fixture.now.UnixMilli()
	mustExecEmulationStationTest(t, fixture.database, `
UPDATE emulationstation_import_items
SET execution_state='COMMIT_FAILED',error_code='INTERNAL_ERROR',retryable=1,
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE import_id=? AND execution_state='PENDING'`, now, now, started.ID)
	mustExecEmulationStationTest(t, fixture.database, `
UPDATE emulationstation_imports
SET state='PARTIAL_FAILURE',phase=NULL,failed_item_count=game_count,retryable=1,
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=?`, now, now, started.ID)
	mustExecEmulationStationTest(t, fixture.database, `
UPDATE jobs SET state='SUCCEEDED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
version=version+1,updated_at_ms=? WHERE id=?`, now, now, unit.JobID)
	failed, err := fixture.service.Get(fixture.context, started.ID)
	testassert.Falsef(t, err != nil || !failed.Retryable,
		"failed summary = %#v, error = %v", failed, err)
	return failed
}

func assertRetryWasNotQueued(t *testing.T, fixture lifecycleFixture, failed Summary) {
	t.Helper()
	current, err := fixture.service.Get(fixture.context, failed.ID)
	testassert.Falsef(t, testassert.Any(
		func() bool { return err != nil },
		func() bool { return current.State != "PARTIAL_FAILURE" },
		func() bool { return current.Version != failed.Version },
	), "current summary = %#v, error = %v", current, err)
	var snapshots int64
	testassert.False(t, fixture.database.QueryRowContext(fixture.context, `
SELECT count(*) FROM job_input_snapshots WHERE job_id=?`, *failed.ImportJobID).
		Scan(&snapshots) != nil, "count retry snapshots")
	testassert.Falsef(t, snapshots != 1, "input snapshots = %d", snapshots)
}
