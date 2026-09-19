package emulationstationimport

import (
	"errors"
	"reflect"
	"testing"

	application "retrom/internal/model/emulationstationimport"
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

			repo := NewRecovery(db)
			candidate, found, err := repo.CurrentRecovery(t.Context(), unit.JobID)
			if err != nil || !found {
				t.Fatalf("current=%v error=%v", found, err)
			}

			if _, err := db.ExecContext(t.Context(), mutation); err != nil {
				t.Fatal(err)
			}
			before := planRows(t, db)

			_, err = repo.CommitRecoveryReviewBatch(t.Context(), candidate, 1500, candidate.ReleaseYearMax)
			if !errors.Is(err, application.ErrVersionConflict) {
				t.Fatalf("changed recovery identity was accepted: %v", err)
			}
			if !reflect.DeepEqual(before, planRows(t, db)) {
				t.Fatal("changed frozen identity was committed")
			}
		})
	}
}
