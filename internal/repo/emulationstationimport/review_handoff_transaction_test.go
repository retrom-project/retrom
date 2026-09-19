package emulationstationimport

import (
	"errors"
	"reflect"
	"testing"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
)

func TestReviewHandoffTransactionDoesNotCommitOnLateFailure(t *testing.T) {
	t.Parallel()
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "callback", true: "commit"}[commit], func(t *testing.T) {
			t.Parallel()
			db, _ := leaseDatabase(t, true)
			before := planRows(t, db)
			err := NewReviewHandoff(db).WithReviewHandoff(t.Context(), func(scope emulationstationimportmodel.ReviewHandoffScope) error {
				records, ok := scope.Write.(executionRecords)
				if !ok {
					t.Fatal("unexpected handoff writer")
				}
				if _, err := records.executor.ExecContext(
					t.Context(),
					`UPDATE emulationstation_imports SET version=version+1`,
				); err != nil {
					return err
				}
				if !commit {
					return errLeaseStorage
				}
				if _, err := records.executor.ExecContext(t.Context(), `PRAGMA defer_foreign_keys=ON`); err != nil {
					return err
				}
				_, err := records.executor.ExecContext(
					t.Context(),
					`INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms) VALUES('missing-handoff-parent',1,'{}','`+planDigest+`',12)`,
				)
				return err
			})
			if err == nil || !commit && !errors.Is(err, errLeaseStorage) {
				t.Fatalf("late error=%v", err)
			}
			if !reflect.DeepEqual(before, planRows(t, db)) {
				t.Fatal("failed handoff committed plan or deferred child")
			}
		})
	}
}

func TestReviewHandoffRepeatsExecutionFence(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{
		"UPDATE jobs SET version=version+1",
		"UPDATE jobs SET worker_id='replacement'",
		"UPDATE jobs SET leased_until_ms=1100",
		"UPDATE jobs SET execution_deadline_at_ms=1100",
		"UPDATE emulationstation_imports SET version=version+1",
		"UPDATE emulationstation_imports SET root_id='replacement'",
		"UPDATE emulationstation_imports SET source_relative_path='replacement'",
		"UPDATE emulationstation_imports SET release_year_max=release_year_max+1",
	} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			db, unit := itemWorkDatabase(t)
			before := planRows(t, db)
			err := NewReviewHandoff(db).WithReviewHandoff(t.Context(), func(scope emulationstationimportmodel.ReviewHandoffScope) error {
				current, found, err := scope.Read.Current(t.Context(), unit.JobID)
				if err != nil || !found {
					t.Fatalf("current=%v error=%v", found, err)
				}
				records, ok := scope.Write.(executionRecords)
				if !ok {
					t.Fatal("unexpected handoff writer")
				}
				if _, err := records.executor.ExecContext(t.Context(), mutation); err != nil {
					t.Fatal(err)
				}
				return scope.Write.CompleteReview(t.Context(), emulationstationimportmodel.ExecutionReviewCompletion{Before: current, NowMS: 1100})
			})
			if !errors.Is(err, emulationstationimportmodel.ErrVersionConflict) {
				t.Fatalf("changed handoff authority=%v", err)
			}
			if !reflect.DeepEqual(before, planRows(t, db)) {
				t.Fatal("changed handoff authority committed")
			}
		})
	}
}
