package pegasusimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	application "retrom/internal/service/pegasusimport"
	"retrom/internal/testkit/testsupport"

	"modernc.org/sqlite"
)

type materialAffectedFailure struct {
	driver.Result
	cause error
}

func (result materialAffectedFailure) RowsAffected() (int64, error) { return 0, result.cause }
func TestMaterializationRejectsFailedOrZeroAffectedRowsAfterActualBinding(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"error", "zero"} {
		t.Run(mode, func(t *testing.T) {
			db, key, blob := materialDatabase(t)
			cause := errors.New("affected-row read failed")
			writes := 0
			fault := testsupport.OpenSQLFaultDatabase(
				t,
				db,
				testsupport.SQLFaultHooks{
					AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
						if strings.HasPrefix(query, "UPDATE pegasus_import_item_files SET blob_id=") && len(args) > 2 && args[2].Value == key.ItemID {
							writes++
							if mode == "error" {
								return materialAffectedFailure{Result: result, cause: cause}, nil
							}
							return driver.RowsAffected(0), nil
						}
						return result, nil
					},
				},
			)
			before := materialRows(t, db)
			service := application.NewMaterialization(NewMaterialization(fault), func() time.Time { return time.UnixMilli(10) })
			source := readMaterialSource(t, NewMaterialization(db), key)
			id, err := service.Copy(t.Context(), materialIdentity(), source, blob)
			expected := cause
			if mode == "zero" {
				expected = application.ErrVersionConflict
			}
			if !errors.Is(err, expected) || id != "" || writes != 1 {
				t.Fatalf("mode=%s id=%s err=%v writes=%d", mode, id, err, writes)
			}
			if !reflect.DeepEqual(before, materialRows(t, db)) {
				t.Fatal("failed changed-row count committed blob/source")
			}
		})
	}
}

func materialIdentity() application.ExecutionIdentity {
	return application.ExecutionIdentity{
		JobID:       "work",
		ImportID:    "import-0",
		WorkerID:    "old-worker",
		ExecutionNo: 1,
		Attempt:     1,
	}
}

func readMaterialSource(t *testing.T, repo *Materialization, key application.MaterialKey) application.MaterialSource {
	t.Helper()
	var source application.MaterialSource
	if err := repo.WithMaterialization(t.Context(), func(scope application.MaterialScope) error {
		before, err := scope.Read.Source(t.Context(), key)
		source = before.Source
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return source
}

type materialCommitFailure struct{ repository *Materialization }

func (repository materialCommitFailure) WithMaterialization(
	ctx context.Context,
	work func(application.MaterialScope) error,
) error {
	return repository.repository.WithMaterialization(ctx, func(scope application.MaterialScope) error {
		if err := work(scope); err != nil {
			return err
		}
		records, ok := scope.Read.(materialRecords)
		if !ok {
			return application.ErrInvalid
		}
		tx := records.tx
		if _, err := tx.ExecContext(
			ctx,
			`CREATE TABLE material_commit_failure(owner TEXT REFERENCES blobs(id) DEFERRABLE INITIALLY DEFERRED)`,
		); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO material_commit_failure VALUES('missing')`)
		return err
	})
}

func TestMaterializationCommitFailureDiscardsResponseAndCatalog(t *testing.T) {
	t.Parallel()
	db, key, blob := materialDatabase(t)
	before := materialRows(t, db)
	source := readMaterialSource(t, NewMaterialization(db), key)
	service := application.NewMaterialization(
		materialCommitFailure{NewMaterialization(db)},
		func() time.Time { return time.UnixMilli(10) },
	)
	id, err := service.Copy(t.Context(), materialIdentity(), source, blob)
	var cause *sqlite.Error
	if id != "" || !errors.As(err, &cause) || !strings.Contains(err.Error(), "commit Pegasus material transaction") {
		t.Fatalf("commit response=%s %v", id, err)
	}
	if !reflect.DeepEqual(before, materialRows(t, db)) {
		t.Fatal("failed commit kept catalog/material")
	}
}

func TestMaterializationPhaseFencesEveryExecutionField(t *testing.T) {
	t.Parallel()
	for _, field := range []string{
		"job version",
		"parent version",
		"execution",
		"attempt",
		"worker",
		"lease",
		"deadline",
		"parent state",
		"phase",
	} {
		t.Run(field, func(t *testing.T) {
			db, _, _ := materialDatabase(t)
			before := materialRows(t, db)
			err := NewMaterialization(db).WithMaterialization(t.Context(), func(scope application.MaterialScope) error {
				current, err := scope.Read.Execution(t.Context(), "work")
				if err != nil {
					return err
				}
				if field == "phase" {
					current.Phase = "changed"
				} else {
					invalidateRecovery(&current.Execution, field)
				}
				return scope.Write.Phase(t.Context(), application.PhaseChange{Before: current, Phase: "VALIDATING", NowMS: 10})
			})
			if !errors.Is(err, application.ErrVersionConflict) {
				t.Fatalf("%s err=%v", field, err)
			}
			if !reflect.DeepEqual(before, materialRows(t, db)) {
				t.Fatalf("%s changed state", field)
			}
		})
	}
}
