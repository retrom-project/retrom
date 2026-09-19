package emulationstationimport

import (
	"errors"
	"reflect"
	"testing"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
)

func TestRecoveryReviewFenceIncludesFrozenSourceIdentity(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{
		"UPDATE emulationstation_imports SET root_id='replacement'",
		"UPDATE emulationstation_imports SET root_config_digest='bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'",
		"UPDATE emulationstation_imports SET source_relative_path='replacement'",
		"UPDATE emulationstation_imports SET release_year_max=release_year_max+1",
	} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			db, unit := recoveryDatabase(t, true, false)
			before := planRows(t, db)
			err := NewRecovery(db).WithRecovery(t.Context(), func(scope emulationstationimportmodel.RecoveryScope) error {
				current, found, err := scope.Read.Current(t.Context(), unit.JobID)
				if err != nil || !found {
					t.Fatalf("current=%v error=%v", found, err)
				}
				records, ok := scope.Write.(recoveryRecords)
				if !ok {
					t.Fatal("unexpected recovery writer")
				}
				if _, err := records.executor.ExecContext(t.Context(), mutation); err != nil {
					t.Fatal(err)
				}
				return scope.Write.Fence(t.Context(), current, 1500)
			})
			if !errors.Is(err, emulationstationimportmodel.ErrVersionConflict) {
				t.Fatalf("changed recovery identity was accepted: %v", err)
			}
			if !reflect.DeepEqual(before, planRows(t, db)) {
				t.Fatal("changed frozen identity was committed")
			}
		})
	}
}
