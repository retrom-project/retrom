package emulationstationimport

import (
	"errors"
	"reflect"
	"testing"

	application "retrom/internal/service/emulationstationimport"
)

func TestCompletionRepeatsFullExecutionFence(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{
		"UPDATE jobs SET worker_id='replacement' WHERE kind='SERVER_EMULATIONSTATION_IMPORT'",
		"UPDATE jobs SET version=version+1 WHERE kind='SERVER_EMULATIONSTATION_IMPORT'",
		"UPDATE jobs SET leased_until_ms=1100 WHERE kind='SERVER_EMULATIONSTATION_IMPORT'",
		"UPDATE jobs SET execution_deadline_at_ms=1100 WHERE kind='SERVER_EMULATIONSTATION_IMPORT'",
		"UPDATE emulationstation_imports SET root_config_digest='bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'",
	} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			db, unit := completionDatabase(t)
			before := planRows(t, db)
			err := NewCompletion(db).WithCompletion(t.Context(), func(scope application.CompletionScope) error {
				execution, found, err := scope.Read.Current(t.Context(), unit.JobID)
				if err != nil || !found {
					t.Fatalf("execution=%v error=%v", found, err)
				}
				counts, err := scope.Read.Counts(t.Context(), unit.ImportID)
				if err != nil {
					t.Fatal(err)
				}
				records, ok := scope.Write.(completionRecords)
				if !ok {
					t.Fatal("unexpected completion writer")
				}
				if _, err := records.executor.ExecContext(t.Context(), mutation); err != nil {
					t.Fatal(err)
				}
				return scope.Write.Complete(
					t.Context(),
					application.CompletionChange{
						Before:      execution,
						Counts:      counts,
						ImportState: "PARTIAL_FAILURE",
						Retryable:   true,
						NowMS:       1100,
					},
				)
			})
			if !errors.Is(err, application.ErrVersionConflict) {
				t.Fatalf("accepted changed ownership: %v", err)
			}
			if !reflect.DeepEqual(before, planRows(t, db)) {
				t.Fatal("completion fence mismatch retained writes")
			}
		})
	}
}
