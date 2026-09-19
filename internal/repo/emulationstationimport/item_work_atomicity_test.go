package emulationstationimport

import (
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
	"retrom/internal/testkit/testsupport"
)

func TestItemWorkSQLAndAffectedRowFailuresRollback(t *testing.T) {
	t.Parallel()
	for _, statement := range []string{
		"UPDATE jobs SET version=version",
		"UPDATE emulationstation_import_items SET",
		"UPDATE emulationstation_imports SET",
		"INSERT INTO job_events",
	} {
		for _, affected := range []bool{false, true} {
			t.Run(statement+map[bool]string{false: "/SQL", true: "/count"}[affected], func(t *testing.T) {
				t.Parallel()
				db, unit := itemWorkDatabase(t)
				item, _, err := itemWorkService(db).Next(t.Context(), unit)
				if err != nil {
					t.Fatal(err)
				}
				before := planRows(t, db)
				target := item.ID
				switch statement {
				case "UPDATE jobs SET version=version", "INSERT INTO job_events":
					target = unit.JobID
				case "UPDATE emulationstation_imports SET":
					target = unit.ImportID
				}
				var hits atomic.Int64
				faultDB := testsupport.OpenSQLFaultDatabase(t, db, scanFaultHooks(statement, target, affected, &hits))
				err = emulationstationimportservice.NewItemWork(NewItemWork(faultDB), func() time.Time { return time.UnixMilli(1100) }).Finish(
					t.Context(),
					unit,
					item.ID,
					emulationstationimportmodel.ItemOutcome{State: "COMMIT_FAILED", Code: "INTERNAL_ERROR"},
				)
				if !errors.Is(err, errLeaseStorage) || hits.Load() != 1 {
					t.Fatalf("cause=%v hits=%d", err, hits.Load())
				}
				if !reflect.DeepEqual(before, planRows(t, db)) {
					t.Fatal("item failure committed partial outcome, counts, event or payload")
				}
			})
		}
	}
}

func TestItemWorkClaimAndOutcomeDoNotEscapeFailedTransaction(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"claim", "outcome"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			db, unit := itemWorkDatabase(t)
			itemID := ""
			if operation == "outcome" {
				item, _, err := itemWorkService(db).Next(t.Context(), unit)
				if err != nil {
					t.Fatal(err)
				}
				itemID = item.ID
			}
			before := planRows(t, db)

			var trigger string
			if operation == "claim" {
				trigger = `CREATE TRIGGER fail_item_work BEFORE UPDATE ON emulationstation_import_items
WHEN NEW.execution_state='COPYING' AND OLD.execution_state='PENDING'
BEGIN SELECT RAISE(ABORT,'injected claim failure'); END`
			} else {
				trigger = `CREATE TRIGGER fail_item_work BEFORE INSERT ON job_events
BEGIN SELECT RAISE(ABORT,'injected outcome failure'); END`
			}
			if _, err := db.ExecContext(t.Context(), trigger); err != nil {
				t.Fatal(err)
			}

			var err error
			if operation == "claim" {
				var item emulationstationimportmodel.ExecutionItem
				var found bool
				item, found, err = itemWorkService(db).Next(t.Context(), unit)
				if item.ID != "" || found {
					t.Fatal("uncommitted claim escaped")
				}
			} else {
				err = itemWorkService(db).Finish(
					t.Context(),
					unit,
					itemID,
					emulationstationimportmodel.ItemOutcome{State: "COMMIT_FAILED", Code: "INTERNAL_ERROR"},
				)
			}
			if err == nil {
				t.Fatal("expected error from injected failure")
			}
			if _, err := db.ExecContext(t.Context(), `DROP TRIGGER IF EXISTS fail_item_work`); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, planRows(t, db)) {
				t.Fatal("failed transaction retained item work")
			}
		})
	}
}
