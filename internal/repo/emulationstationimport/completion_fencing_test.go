package emulationstationimport

import (
	"errors"
	"reflect"
	"testing"

	application "retrom/internal/model/emulationstationimport"
)

func TestCompletionRepeatsFullExecutionFence(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{
		"UPDATE jobs SET worker_id='replacement' WHERE kind='SERVER_EMULATIONSTATION_IMPORT'",
		"UPDATE jobs SET leased_until_ms=1100 WHERE kind='SERVER_EMULATIONSTATION_IMPORT'",
		"UPDATE jobs SET execution_deadline_at_ms=1100 WHERE kind='SERVER_EMULATIONSTATION_IMPORT'",
		"UPDATE emulationstation_imports SET root_config_digest='bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'",
	} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			db, unit := completionDatabase(t)
			if _, err := db.ExecContext(t.Context(), mutation); err != nil {
				t.Fatal(err)
			}
			before := planRows(t, db)
			err := NewCompletion(db).CommitCompletion(t.Context(), unit, 1100)
			if !errors.Is(err, application.ErrVersionConflict) {
				t.Fatalf("accepted changed ownership: %v", err)
			}
			if !reflect.DeepEqual(before, planRows(t, db)) {
				t.Fatal("completion fence mismatch retained writes")
			}
		})
	}
}
