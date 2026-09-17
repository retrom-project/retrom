package emulationstationimport

import (
	"errors"
	"reflect"
	"testing"

	application "retrom/internal/model/emulationstationimport"
)

func TestScanWriteFenceRejectsChangedDurableIdentity(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{
		"UPDATE jobs SET version=version+1", "UPDATE jobs SET execution_no=execution_no+1",
		"UPDATE jobs SET attempt_count=attempt_count+1", "UPDATE jobs SET worker_id='replacement'",
		"UPDATE jobs SET leased_until_ms=1001", "UPDATE jobs SET execution_deadline_at_ms=1001",
		"UPDATE jobs SET execution_started_at_ms=999", "UPDATE jobs SET state='CANCEL_REQUESTED',cancel_requested_at_ms=1001",
		"UPDATE emulationstation_imports SET version=version+1",
		"UPDATE emulationstation_imports SET root_id='replacement'",
		"UPDATE emulationstation_imports SET root_config_digest='bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'",
		"UPDATE emulationstation_imports SET source_relative_path='replacement'",
		"UPDATE emulationstation_imports SET release_year_max=release_year_max+1",
	} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			db, unit, value := scanDatabase(t)
			before := planRows(t, db)
			err := NewScanPublication(db).WithScan(t.Context(), func(scope application.ScanScope) error {
				snapshot, found, err := scope.Read.Current(t.Context(), unit.JobID)
				if err != nil || !found {
					t.Fatalf("snapshot=%v %v", found, err)
				}
				records, ok := scope.Write.(scanRecords)
				if !ok {
					t.Fatal("unexpected scan writer")
				}
				if _, err := records.executor.ExecContext(t.Context(), mutation); err != nil {
					t.Fatal(err)
				}
				return scope.Write.Headers(t.Context(), application.ScanMutation{Before: snapshot, NowMS: 1001}, value)
			})
			if !errors.Is(err, application.ErrVersionConflict) {
				t.Fatalf("stale mutation accepted: %v", err)
			}
			if !reflect.DeepEqual(before, planRows(t, db)) {
				t.Fatal("rejected fence changed projection")
			}
		})
	}
}
