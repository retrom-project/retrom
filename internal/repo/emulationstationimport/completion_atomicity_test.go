package emulationstationimport

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	application "retrom/internal/service/emulationstationimport"
	"retrom/internal/testkit/testsupport"
)

func TestCompletionSQLAndAffectedFailuresRollback(t *testing.T) {
	t.Parallel()
	for _, statement := range []string{
		"UPDATE jobs SET version=version",
		"UPDATE emulationstation_imports SET",
		"UPDATE jobs SET state='SUCCEEDED'",
		"INSERT INTO job_events",
	} {
		for _, affected := range []bool{false, true} {
			t.Run(statement+map[bool]string{false: "/SQL", true: "/count"}[affected], func(t *testing.T) {
				t.Parallel()
				db, unit := completionDatabase(t)
				before := planRows(t, db)
				target := unit.JobID
				if statement == "UPDATE emulationstation_imports SET" {
					target = unit.ImportID
				}
				var hits atomic.Int64
				faultDB := testsupport.OpenSQLFaultDatabase(t, db, scanFaultHooks(statement, target, affected, &hits))
				err := completionService(faultDB).Finish(t.Context(), unit)
				if !errors.Is(err, errLeaseStorage) || hits.Load() != 1 {
					t.Fatalf("cause=%v hits=%d", err, hits.Load())
				}
				if !reflect.DeepEqual(before, planRows(t, db)) {
					t.Fatal("failed completion retained counts, payload, job or event")
				}
			})
		}
	}
}

type completionLateFailure struct {
	*Completion
	commit bool
}

func (repository completionLateFailure) WithCompletion(
	ctx context.Context,
	run func(application.CompletionScope) error,
) error {
	return repository.Completion.WithCompletion(ctx, func(scope application.CompletionScope) error {
		if err := run(scope); err != nil {
			return err
		}
		if !repository.commit {
			return errLeaseStorage
		}
		records, ok := scope.Write.(completionRecords)
		if !ok {
			return errors.New("unexpected completion writer")
		}
		if _, err := records.executor.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON`); err != nil {
			return err
		}
		_, err := records.executor.ExecContext(
			ctx,
			`INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms) VALUES('missing-completion-parent',1,'{}','`+planDigest+`',12)`,
		)
		return err
	})
}

func TestCompletionCallbackAndCommitFailuresRollback(t *testing.T) {
	t.Parallel()
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "callback", true: "commit"}[commit], func(t *testing.T) {
			t.Parallel()
			db, unit := completionDatabase(t)
			before := planRows(t, db)
			service := application.NewCompletion(
				completionLateFailure{Completion: NewCompletion(db), commit: commit},
				func() time.Time { return time.UnixMilli(1100) },
			)
			err := service.Finish(t.Context(), unit)
			if err == nil || !commit && !errors.Is(err, errLeaseStorage) {
				t.Fatalf("late failure=%v", err)
			}
			if !reflect.DeepEqual(before, planRows(t, db)) {
				t.Fatal("failed commit retained completion")
			}
		})
	}
}
