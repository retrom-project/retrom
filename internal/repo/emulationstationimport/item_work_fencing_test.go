package emulationstationimport

import (
	"errors"
	"reflect"
	"testing"

	application "retrom/internal/model/emulationstationimport"
)

func TestItemWorkRepeatsFullExecutionAndItemFence(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{
		"worker", "lease", "deadline",
	} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			db, unit := itemWorkDatabase(t)

			switch mutation {
			case "worker":
				if _, err := db.ExecContext(t.Context(), `UPDATE jobs SET worker_id='replacement' WHERE kind='SERVER_EMULATIONSTATION_IMPORT'`); err != nil {
					t.Fatal(err)
				}
			case "lease":
				if _, err := db.ExecContext(t.Context(), `UPDATE jobs SET leased_until_ms=1 WHERE kind='SERVER_EMULATIONSTATION_IMPORT'`); err != nil {
					t.Fatal(err)
				}
			case "deadline":
				if _, err := db.ExecContext(t.Context(), `UPDATE jobs SET execution_deadline_at_ms=1 WHERE kind='SERVER_EMULATIONSTATION_IMPORT'`); err != nil {
					t.Fatal(err)
				}
			}

			before := planRows(t, db)
			_, err := NewItemWork(db).ClaimNextItem(t.Context(), unit, 1100)
			if !errors.Is(err, application.ErrVersionConflict) {
				t.Fatalf("accepted changed ownership for %s: %v", mutation, err)
			}
			if !reflect.DeepEqual(before, planRows(t, db)) {
				t.Fatal("fence mismatch retained writes")
			}
		})
	}
}
