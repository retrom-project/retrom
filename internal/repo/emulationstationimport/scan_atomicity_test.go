package emulationstationimport

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	application "retrom/internal/service/emulationstationimport"
	"retrom/internal/testkit/testsupport"
)

func TestScanWriteAndRowCountFailuresRollBackCurrentTransaction(t *testing.T) {
	t.Parallel()
	for _, step := range []struct{ operation, statement, target string }{
		{"headers", "INSERT INTO emulationstation_import_gamelists(", "import-0"},
		{"headers", "INSERT INTO emulationstation_import_collections(", "collection"},
		{"items", "INSERT INTO emulationstation_import_items(", "item"},
		{"items", "INSERT INTO emulationstation_import_item_files(", "item"},
		{"items", "INSERT INTO emulationstation_import_item_assets(", "item"},
		{"finish", "UPDATE emulationstation_imports SET", "import-0"},
		{"finish", "UPDATE jobs SET state=", "job-0"},
		{"finish", "INSERT INTO job_events(", "job-0"},
		{"reset", "DELETE FROM emulationstation_import_item_assets", "import-0"},
		{"reset", "DELETE FROM emulationstation_import_items", "import-0"},
		{"reset", "UPDATE emulationstation_imports SET", "import-0"},
		{"reject", "UPDATE emulationstation_imports SET", "import-0"},
	} {
		modes := []bool{false, true}
		if strings.HasPrefix(step.statement, "INSERT INTO emulationstation_") {
			modes = []bool{false}
		}
		for _, affected := range modes {
			t.Run(step.operation+step.statement+map[bool]string{false: "/SQL", true: "/RowsAffected"}[affected], func(t *testing.T) {
				t.Parallel()
				assertScanAtomicity(t, step.operation, step.statement, step.target, affected)
			})
		}
	}
}

func prepareScanOperation(t *testing.T, db *sql.DB, unit application.Execution, value application.ScanProjection, operation string) {
	t.Helper()
	service := scanService(db)
	if operation == "items" || operation == "finish" || operation == "reset" {
		if err := service.Headers(t.Context(), unit, value); err != nil {
			t.Fatal(err)
		}
	}
	if operation == "finish" || operation == "reset" {
		if err := service.Items(t.Context(), unit, value.Items); err != nil {
			t.Fatal(err)
		}
	}
}

func runScanOperation(ctx context.Context, service *application.ScanPublication, unit application.Execution, value application.ScanProjection, operation string) error {
	switch operation {
	case "headers":
		return service.Headers(ctx, unit, value)
	case "items":
		return service.Items(ctx, unit, value.Items)
	case "finish":
		return service.Finish(ctx, unit, value)
	case "reset":
		return service.Reset(ctx, unit)
	case "reject":
		return service.Rejected(ctx, unit, value)
	default:
		return errors.New("unexpected scan operation")
	}
}

func assertScanAtomicity(t *testing.T, operation, statement, target string, affected bool) {
	t.Helper()
	db, unit, value := scanDatabase(t)
	prepareScanOperation(t, db, unit, value, operation)
	before := planRows(t, db)
	var hits atomic.Int64
	faultDB := testsupport.OpenSQLFaultDatabase(t, db, scanFaultHooks(statement, target, affected, &hits))
	err := runScanOperation(t.Context(), scanService(faultDB), unit, value, operation)
	if !errors.Is(err, errLeaseStorage) || hits.Load() != 1 {
		t.Fatalf("fault=%v hits=%d", err, hits.Load())
	}
	if !reflect.DeepEqual(before, planRows(t, db)) {
		t.Fatal("failed scan transaction changed durable projection")
	}
}

func scanFaultHooks(statement, target string, affected bool, hits *atomic.Int64) testsupport.SQLFaultHooks {
	match := func(query string, args []driver.NamedValue) bool {
		if !strings.HasPrefix(strings.Join(strings.Fields(query), " "), statement) {
			return false
		}
		for _, arg := range args {
			if arg.Value == target {
				return true
			}
		}
		return false
	}
	return testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
			if !affected && match(query, args) {
				hits.Add(1)
				return errLeaseStorage
			}
			return nil
		},
		BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
			if !affected && match(query, args) {
				hits.Add(1)
				return errLeaseStorage
			}
			return nil
		},
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if affected && match(query, args) {
				hits.Add(1)
				return leaseResultFault{Result: result}, nil
			}
			return result, nil
		},
	}
}

func TestScanItemBatchFailurePreservesPriorBatchAndResetRemovesIt(t *testing.T) {
	t.Parallel()
	db, unit, value := scanDatabase(t)
	value = expandScanItems(value, 1001)
	if err := scanService(db).Headers(t.Context(), unit, value); err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int64
	faultDB := testsupport.OpenSQLFaultDatabase(t, db, scanFaultHooks("INSERT INTO emulationstation_import_items(", "item-0500", false, &hits))
	if err := scanService(faultDB).Items(t.Context(), unit, value.Items); !errors.Is(err, errLeaseStorage) || hits.Load() != 1 {
		t.Fatalf("batch cause=%v hits=%d", err, hits.Load())
	}
	var items, files, assets, events int
	if err := db.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM emulationstation_import_items),(SELECT count(*) FROM emulationstation_import_item_files),(SELECT count(*) FROM emulationstation_import_item_assets),(SELECT count(*) FROM job_events WHERE event_type='SUCCEEDED')`).Scan(&items, &files, &assets, &events); err != nil {
		t.Fatal(err)
	}
	if items != 500 || files != 500 || assets != 500 || events != 0 {
		t.Fatalf("partial batch=%d/%d/%d success=%d", items, files, assets, events)
	}
	if err := scanService(db).Reset(t.Context(), unit); err != nil {
		t.Fatal(err)
	}
	assertClearedScan(t, db, unit, false)
}
