package emulationstationimport

import (
	"errors"
	"reflect"
	"testing"

	application "retrom/internal/model/emulationstationimport"
)

func TestExecutionFinishFenceRejectsEveryChangedAuthority(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{
		"UPDATE jobs SET version=version+1", "UPDATE jobs SET execution_no=execution_no+1",
		"UPDATE jobs SET attempt_count=attempt_count+1", "UPDATE jobs SET worker_id='replacement'",
		"UPDATE jobs SET max_attempts=3", "UPDATE jobs SET leased_until_ms=1002",
		"UPDATE jobs SET execution_deadline_at_ms=1002", "UPDATE jobs SET execution_started_at_ms=999",
		"UPDATE jobs SET state='CANCEL_REQUESTED',cancel_requested_at_ms=1002",
		"UPDATE emulationstation_imports SET version=version+1",
		"UPDATE emulationstation_imports SET root_id='replacement'",
		"UPDATE emulationstation_imports SET root_config_digest='bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'",
		"UPDATE emulationstation_imports SET source_relative_path='replacement'",
		"UPDATE emulationstation_imports SET release_year_max=release_year_max+1",
	} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			db, unit := executionTestDatabase(t, "fail")
			before := planRows(t, db)
			err := NewExecutionControl(db).WithExecution(t.Context(), func(scope application.ExecutionScope) error {
				current, found, err := scope.Read.Current(t.Context(), unit.JobID)
				if err != nil || !found {
					t.Fatalf("current=%v error=%v", found, err)
				}
				records, ok := scope.Write.(executionRecords)
				if !ok {
					t.Fatal("unexpected execution writer")
				}
				if _, err := records.executor.ExecContext(t.Context(), mutation); err != nil {
					t.Fatal(err)
				}
				return scope.Write.Finish(t.Context(), application.ExecutionFinish{
					Before: current, NowMS: 1002,
					JobState: "FAILED", ImportState: "FAILED", ItemState: "COMMIT_FAILED", Code: "INTERNAL_ERROR",
				})
			})
			if !errors.Is(err, application.ErrVersionConflict) {
				t.Fatalf("stale authority accepted=%v", err)
			}
			if !reflect.DeepEqual(before, planRows(t, db)) {
				t.Fatal("stale write was not rolled back")
			}
		})
	}
}
