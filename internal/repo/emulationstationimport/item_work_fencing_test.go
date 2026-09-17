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
		"UPDATE jobs SET worker_id='replacement' WHERE kind='SERVER_EMULATIONSTATION_IMPORT'",
		"UPDATE jobs SET version=version+1 WHERE kind='SERVER_EMULATIONSTATION_IMPORT'",
		"UPDATE jobs SET leased_until_ms=1100 WHERE kind='SERVER_EMULATIONSTATION_IMPORT'",
		"UPDATE jobs SET execution_deadline_at_ms=1100 WHERE kind='SERVER_EMULATIONSTATION_IMPORT'",
		"UPDATE emulationstation_imports SET root_config_digest='bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'",
		"UPDATE emulationstation_import_items SET version=version+1 WHERE id='start-item-2'",
		"UPDATE emulationstation_import_items SET metadata_json='{}' WHERE id='start-item-2'",
	} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			db, unit := itemWorkDatabase(t)
			before := planRows(t, db)
			err := NewItemWork(db).WithItemWork(t.Context(), func(scope application.ItemWorkScope) error {
				execution, found, err := scope.Read.Current(t.Context(), unit.JobID)
				if err != nil || !found {
					t.Fatalf("execution=%v error=%v", found, err)
				}
				item, found, err := scope.Read.Next(t.Context(), unit.ImportID)
				if err != nil || !found {
					t.Fatalf("item=%v error=%v", found, err)
				}
				records, ok := scope.Write.(itemWorkRecords)
				if !ok {
					t.Fatal("unexpected item writer")
				}
				if _, err := records.executor.ExecContext(t.Context(), mutation); err != nil {
					t.Fatal(err)
				}
				return scope.Write.Claim(
					t.Context(),
					application.ItemClaim{Before: application.OwnedItem{Execution: execution, Item: item}, NowMS: 1100},
				)
			})
			if !errors.Is(err, application.ErrVersionConflict) {
				t.Fatalf("accepted changed ownership: %v", err)
			}
			if !reflect.DeepEqual(before, planRows(t, db)) {
				t.Fatal("fence mismatch retained writes")
			}
		})
	}
}
