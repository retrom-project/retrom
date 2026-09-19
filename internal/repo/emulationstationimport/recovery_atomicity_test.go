package emulationstationimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
	"retrom/internal/testkit/testsupport"
)

func TestRecoverySQLFailuresRollBackWholeExecution(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"list", "current", "fence", "assets", "files", "items", "collections", "gamelists", "job", "aggregate", "event", "affected rows", "deleted rows", "import items", "payload"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			assertRecoverySQLRollback(t, stage)
		})
	}
}

func assertRecoverySQLRollback(t *testing.T, stage string) {
	t.Helper()
	importing := stage == "payload" || stage == "import items"
	db, unit := recoveryDatabase(t, importing, !importing)
	if _, err := db.ExecContext(t.Context(), `UPDATE jobs SET execution_deadline_at_ms=2000 WHERE id=?`, unit.JobID); err != nil {
		t.Fatal(err)
	}
	before := planRows(t, db)
	var hits atomic.Int64
	faultDB := testsupport.OpenSQLFaultDatabase(t, db, recoveryFaultHooks(stage, unit, &hits))
	err := emulationstationimportservice.NewRecovery(NewRecovery(faultDB), func() time.Time { return time.UnixMilli(1500) }).Recover(t.Context())
	if !errors.Is(err, errLeaseStorage) || hits.Load() != 1 {
		t.Fatalf("stage=%s cause=%v hits=%d", stage, err, hits.Load())
	}
	if !reflect.DeepEqual(before, planRows(t, db)) {
		t.Fatal("failed recovery retained partial state")
	}
}

func recoveryFaultHooks(stage string, unit emulationstationimportmodel.Execution, hits *atomic.Int64) testsupport.SQLFaultHooks {
	prefixes := map[string]string{"fence": "UPDATE jobs SET version=version", "assets": "DELETE FROM emulationstation_import_item_assets", "files": "DELETE FROM emulationstation_import_item_files", "items": "DELETE FROM emulationstation_import_items", "collections": "DELETE FROM emulationstation_import_collections", "gamelists": "DELETE FROM emulationstation_import_gamelists", "job": "UPDATE jobs SET state=", "aggregate": "UPDATE emulationstation_imports SET", "event": "INSERT INTO job_events(", "payload": "INSERT INTO jobs(", "import items": "UPDATE emulationstation_import_items SET"}
	return testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
			match := stage == "list" && strings.Contains(query, "ORDER BY job.leased_until_ms") || stage == "current" && query == leaseSnapshotSQL+` AND job.id=?`
			if match && recoveryBoundTarget(stage, args, unit) {
				hits.Add(1)
				return errLeaseStorage
			}
			return nil
		},
		BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
			prefix, ok := prefixes[stage]
			if ok && strings.HasPrefix(strings.Join(strings.Fields(query), " "), prefix) && recoveryBoundTarget(stage, args, unit) {
				hits.Add(1)
				return errLeaseStorage
			}
			return nil
		},
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			match := stage == "affected rows" && strings.HasPrefix(query, "UPDATE jobs SET version=version") || stage == "deleted rows" && strings.HasPrefix(query, "DELETE FROM emulationstation_import_item_assets")
			if match && recoveryBoundTarget(stage, args, unit) {
				hits.Add(1)
				return leaseResultFault{Result: result}, nil
			}
			return result, nil
		},
	}
}

func TestRecoveryCommitFailureRollsBack(t *testing.T) {
	t.Parallel()
	db, _ := recoveryDatabase(t, false, true)
	before := planRows(t, db)

	cause := errors.New("recovery commit injected")
	faultDB := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, _ []driver.NamedValue) error {
			if strings.TrimSpace(query) == "COMMIT" {
				return cause
			}
			return nil
		},
	})
	err := emulationstationimportservice.NewRecovery(NewRecovery(faultDB), func() time.Time { return time.UnixMilli(1500) }).Recover(t.Context())
	if err == nil {
		t.Fatal("late recovery failure committed")
	}
	if !reflect.DeepEqual(before, planRows(t, db)) {
		t.Fatal("late failure retained projection changes")
	}
}

func recoveryBoundTarget(stage string, args []driver.NamedValue, unit emulationstationimportmodel.Execution) bool {
	if stage == "list" {
		return len(args) == 3 && args[0].Value == int64(1500) && args[1].Value == int64(1500) && args[2].Value == int64(100)
	}
	id := unit.ImportID
	switch stage {
	case "current", "fence", "job", "event", "affected rows":
		id = unit.JobID
	case "payload":
		id = "EMULATIONSTATION_IMPORT_ITEM"
	}
	for _, arg := range args {
		if arg.Value == id {
			return true
		}
	}
	return false
}
