package emulationstationimport

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	application "retrom/internal/service/emulationstationimport"
	"retrom/internal/testsupport"
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
				err = application.NewItemWork(NewItemWork(faultDB), func() time.Time { return time.UnixMilli(1100) }).Finish(
					t.Context(),
					unit,
					item.ID,
					application.ItemOutcome{State: "COMMIT_FAILED", Code: "INTERNAL_ERROR"},
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

type itemWorkLateFailure struct {
	*ItemWork
	commit bool
}

func (repository itemWorkLateFailure) WithItemWork(
	ctx context.Context,
	run func(application.ItemWorkScope) error,
) error {
	return repository.ItemWork.WithItemWork(ctx, func(scope application.ItemWorkScope) error {
		if err := run(scope); err != nil {
			return err
		}
		if !repository.commit {
			return errLeaseStorage
		}
		records, ok := scope.Write.(itemWorkRecords)
		if !ok {
			return errors.New("unexpected item writer")
		}
		if _, err := records.executor.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON`); err != nil {
			return err
		}
		_, err := records.executor.ExecContext(
			ctx,
			`INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms) VALUES('missing-item-parent',1,'{}','`+planDigest+`',12)`,
		)
		return err
	})
}

func TestItemWorkClaimAndOutcomeDoNotEscapeFailedCommit(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"claim", "outcome"} {
		for _, commit := range []bool{false, true} {
			t.Run(operation+map[bool]string{false: "/callback", true: "/commit"}[commit], func(t *testing.T) {
				t.Parallel()
				assertItemWorkLateFailure(t, operation, commit)
			})
		}
	}
}

func assertItemWorkLateFailure(t *testing.T, operation string, commit bool) {
	t.Helper()
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
	service := application.NewItemWork(
		itemWorkLateFailure{ItemWork: NewItemWork(db), commit: commit},
		func() time.Time { return time.UnixMilli(1100) },
	)
	var err error
	if operation == "claim" {
		var item application.ExecutionItem
		var found bool
		item, found, err = service.Next(t.Context(), unit)
		if item.ID != "" || found {
			t.Fatal("uncommitted claim escaped")
		}
	} else {
		err = service.Finish(
			t.Context(),
			unit,
			itemID,
			application.ItemOutcome{State: "COMMIT_FAILED", Code: "INTERNAL_ERROR"},
		)
	}
	if err == nil || !commit && !errors.Is(err, errLeaseStorage) {
		t.Fatalf("late failure=%v", err)
	}
	if !reflect.DeepEqual(before, planRows(t, db)) {
		t.Fatal("failed commit retained item work")
	}
}
