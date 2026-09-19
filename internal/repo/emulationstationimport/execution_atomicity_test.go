package emulationstationimport

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
	"retrom/internal/testkit/testsupport"
)

func executionTestDatabase(t *testing.T, operation string) (*sql.DB, emulationstationimportmodel.Execution) {
	t.Helper()
	db, unit, projection := scanDatabase(t)
	if operation != "cancel" {
		return db, unit
	}
	{
		if err := scanService(db).Headers(t.Context(), unit, projection); err != nil {
			t.Fatal(err)
		}
		if err := scanService(db).Items(t.Context(), unit, projection.Items); err != nil {
			t.Fatal(err)
		}
		summary, err := NewQueries(db).Get(t.Context(), unit.ImportID)
		if err != nil {
			t.Fatal(err)
		}
		if _, pending, err := scanCancellationService(db).Cancel(
			t.Context(),
			unit.ImportID,
			summary.Version,
			"Stop",
			"actor",
		); err != nil || !pending {
			t.Fatalf("cancel request=%v error=%v", pending, err)
		}
	}
	return db, unit
}

func executeControlOperation(
	t *testing.T,
	repository emulationstationimportmodel.ExecutionRepository,
	unit emulationstationimportmodel.Execution,
	operation string,
) error {
	t.Helper()
	service := emulationstationimportservice.NewExecutionControl(repository, func() time.Time { return time.UnixMilli(1002) })
	if operation == "cancel" {
		closed, err := service.CloseCancelled(t.Context(), unit)
		if err != nil && closed {
			t.Fatal("returned acknowledgement before commit")
		}
		return err
	}
	state, err := service.Fail(t.Context(), unit, emulationstationimportmodel.ExecutionFailure{Code: "INTERNAL_ERROR", Retryable: operation == "retry"})
	if err != nil && state != "" {
		t.Fatal("returned failure result before commit")
	}
	return err
}

func TestExecutionControlSQLAndRowsAffectedFailuresRollBack(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"cancel", "retry", "fail"} {
		for _, statement := range []string{
			"UPDATE jobs SET version=version",
			"UPDATE jobs SET state=",
			"UPDATE emulationstation_imports SET",
			"INSERT INTO job_events",
		} {
			for _, affected := range []bool{false, true} {
				t.Run(operation+statement+map[bool]string{false: "/SQL", true: "/count"}[affected], func(t *testing.T) {
					t.Parallel()
					db, unit := executionTestDatabase(t, operation)
					before := planRows(t, db)
					var hits atomic.Int64
					target := unit.JobID
					if statement == "UPDATE emulationstation_imports SET" {
						target = unit.ImportID
					}
					faultDB := testsupport.OpenSQLFaultDatabase(t, db, scanFaultHooks(statement, target, affected, &hits))
					err := executeControlOperation(t, NewExecutionControl(faultDB), unit, operation)
					if !errors.Is(err, errLeaseStorage) || hits.Load() != 1 {
						t.Fatalf("cause=%v hits=%d", err, hits.Load())
					}
					if !reflect.DeepEqual(before, planRows(t, db)) {
						t.Fatal("failure retained execution writes")
					}
				})
			}
		}
	}
}

func TestExecutionControlCommitFailuresRollBack(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"cancel", "retry", "fail"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			db, unit := executionTestDatabase(t, operation)
			before := planRows(t, db)
			faultDB := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{
				BeforeExec: func(_ context.Context, query string, _ []driver.NamedValue) error {
					if query == "COMMIT" {
						return errLeaseStorage
					}
					return nil
				},
			})
			err := executeControlOperation(t, NewExecutionControl(faultDB), unit, operation)
			if err == nil || !errors.Is(err, errLeaseStorage) {
				t.Fatalf("commit failure=%v", err)
			}
			if !reflect.DeepEqual(before, planRows(t, db)) {
				t.Fatal("commit failure retained execution writes")
			}
		})
	}
}
