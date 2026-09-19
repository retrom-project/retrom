package emulationstationimport

import (
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	emulationstationimportservice "retrom/internal/service/emulationstationimport"
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

func TestCompletionCommitFailureRollback(t *testing.T) {
	t.Parallel()
	db, unit := completionDatabase(t)
	before := planRows(t, db)
	if _, err := db.ExecContext(t.Context(),
		`CREATE TRIGGER completion_fault AFTER INSERT ON job_events BEGIN
SELECT RAISE(ABORT, 'injected completion failure'); END`); err != nil {
		t.Fatal(err)
	}
	service := emulationstationimportservice.NewCompletion(
		NewCompletion(db),
		func() time.Time { return time.UnixMilli(1100) },
	)
	err := service.Finish(t.Context(), unit)
	if err == nil {
		t.Fatal("commit failure not propagated")
	}
	if _, err := db.ExecContext(t.Context(), `DROP TRIGGER completion_fault`); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, planRows(t, db)) {
		t.Fatal("failed commit retained completion")
	}
}
