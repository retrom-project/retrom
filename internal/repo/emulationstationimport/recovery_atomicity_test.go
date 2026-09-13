package emulationstationimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	application "retrom/internal/service/emulationstationimport"
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
	err := application.NewRecovery(NewRecovery(faultDB), func() time.Time { return time.UnixMilli(1500) }).Recover(t.Context())
	if !errors.Is(err, errLeaseStorage) || hits.Load() != 1 {
		t.Fatalf("stage=%s cause=%v hits=%d", stage, err, hits.Load())
	}
	if !reflect.DeepEqual(before, planRows(t, db)) {
		t.Fatal("failed recovery retained partial state")
	}
}

func recoveryFaultHooks(stage string, unit application.Execution, hits *atomic.Int64) testsupport.SQLFaultHooks {
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

type recoveryCompletionFailure struct {
	*Recovery
	commit bool
}

func (repository recoveryCompletionFailure) WithRecovery(ctx context.Context, work func(application.RecoveryScope) error) error {
	return repository.Recovery.WithRecovery(ctx, func(scope application.RecoveryScope) error {
		if err := work(scope); err != nil {
			return err
		}
		if !repository.commit {
			return errLeaseStorage
		}
		records, ok := scope.Read.(recoveryRecords)
		if !ok {
			return errors.New("unexpected recovery scope")
		}
		if _, err := records.executor.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON`); err != nil {
			return fmt.Errorf("defer recovery fixture constraint: %w", err)
		}
		if _, err := records.executor.ExecContext(ctx, `INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES('missing-recovery-parent',1,'{}','`+planDigest+`',12)`); err != nil {
			return fmt.Errorf("inject recovery commit fault: %w", err)
		}
		return nil
	})
}

func TestRecoveryRollsBackAfterFinalEventAndCommitFailure(t *testing.T) {
	t.Parallel()
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "callback", true: "commit"}[commit], func(t *testing.T) {
			t.Parallel()
			db, _ := recoveryDatabase(t, false, true)
			before := planRows(t, db)
			err := application.NewRecovery(recoveryCompletionFailure{Recovery: NewRecovery(db), commit: commit}, func() time.Time { return time.UnixMilli(1500) }).Recover(t.Context())
			if err == nil {
				t.Fatal("late recovery failure committed")
			}
			if !commit && !errors.Is(err, errLeaseStorage) {
				t.Fatalf("lost callback cause: %v", err)
			}
			if commit && !strings.Contains(err.Error(), "FOREIGN KEY") {
				t.Fatalf("missing commit failure: %v", err)
			}
			if !reflect.DeepEqual(before, planRows(t, db)) {
				t.Fatal("late failure retained projection changes")
			}
		})
	}
}

func recoveryBoundTarget(stage string, args []driver.NamedValue, unit application.Execution) bool {
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
