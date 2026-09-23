//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"

	"retrom/internal/testsupport"
)

func TestImportCreationRejectsMissingReturnedIdentity(t *testing.T) {
	for _, table := range []string{"upload_consumptions"} {
		t.Run(table, func(t *testing.T) {
			service, plan := preparedCommitFixture(t)
			inserted := 0
			service.database = testsupport.OpenSQLFaultDatabase(t, service.database, testsupport.SQLFaultHooks{
				AfterQuery: func(_ context.Context, query string, _ []driver.NamedValue, rows driver.Rows) (driver.Rows, error) {
					if !strings.HasPrefix(strings.TrimSpace(query), "INSERT INTO "+table+"(") {
						return rows, nil
					}
					// Advance the real RETURNING cursor so the write actually happens before hiding its key.
					values := make([]driver.Value, len(rows.Columns()))
					if err := rows.Next(values); err != nil {
						return nil, err
					}
					inserted++
					return creationEmptyRows{Rows: rows}, nil
				},
			})
			before := creationEffectCounts(t, service.database)
			result, err := commitPreparedFixture(t.Context(), service, plan, nil)
			if err == nil || result != (Created{}) || inserted != 1 {
				t.Fatalf("missing %s identity committed: result=%+v writes=%d error=%v", table, result, inserted, err)
			}
			assertCreationEffectsUnchanged(t, service.database, before)
		})
	}
}

type creationEmptyRows struct{ driver.Rows }

func (creationEmptyRows) Next([]driver.Value) error { return io.EOF }

type creationFaultResult struct {
	driver.Result
	cause error
}

func (result creationFaultResult) RowsAffected() (int64, error) { return 0, result.cause }

func TestImportCreationRejectsUncheckedWriteCount(t *testing.T) {
	for _, table := range []string{"import_item_source_snapshot_files", "job_events"} {
		for _, mode := range []string{"zero", "cause"} {
			t.Run(table+"/"+mode, func(t *testing.T) { assertCreationCountFault(t, table, mode) })
		}
	}
}

func assertCreationCountFault(t *testing.T, table, mode string) {
	t.Helper()
	service, plan := preparedCommitFixture(t)
	cause := errors.New("creation write count unavailable")
	var injected error
	if mode == "cause" {
		injected = cause
	}
	preceding, writes := 0, 0
	service.database = testsupport.OpenSQLFaultDatabase(t, service.database, testsupport.SQLFaultHooks{
		AfterExec: func(_ context.Context, query string, _ []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.HasPrefix(strings.TrimSpace(query), "INSERT INTO import_items(") {
				count, err := result.RowsAffected()
				if err != nil {
					return nil, err
				}
				preceding += int(count)
			}
			if !strings.HasPrefix(strings.TrimSpace(query), "INSERT INTO "+table+"(") {
				return result, nil
			}
			count, err := result.RowsAffected()
			if err != nil {
				return nil, err
			}
			writes += int(count)
			return creationFaultResult{Result: result, cause: injected}, nil
		},
	})
	before := creationEffectCounts(t, service.database)
	result, err := commitPreparedFixture(t.Context(), service, plan, nil)
	if err == nil || result != (Created{}) || preceding != 1 || writes < 1 || mode == "cause" && !errors.Is(err, cause) {
		t.Fatalf("unchecked %s/%s: result=%+v preceding=%d writes=%d error=%v", table, mode, result, preceding, writes, err)
	}
	assertCreationEffectsUnchanged(t, service.database, before)
}
