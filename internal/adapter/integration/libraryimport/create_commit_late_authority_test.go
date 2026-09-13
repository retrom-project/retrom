//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/testkit/testsupport"
)

func TestImportCreationRollsBackWhenLeaseExpiresAfterSourceWrite(t *testing.T) {
	service, plan := preparedCommitFixture(t)
	var clock atomic.Int64
	clock.Store(time.Now().UnixMilli())
	service.now = func() time.Time { return time.UnixMilli(clock.Load()) }
	admissions := application.NewImportAdmissions(repository.NewImportAdmissions(service.database), nil, service.tags,
		application.ImportAdmissionOptions{Now: service.now})
	created, err := admissions.Queue(t.Context(), plan.Request)
	if err != nil {
		t.Fatal(err)
	}
	work, err := service.claimImportGroup(t.Context(), created.JobID)
	if err != nil {
		t.Fatal(err)
	}
	written := 0
	service.database = testsupport.OpenSQLFaultDatabase(t, service.database, testsupport.SQLFaultHooks{
		AfterExec: func(_ context.Context, query string, _ []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.HasPrefix(strings.TrimSpace(query), "INSERT INTO import_items(") {
				count, err := result.RowsAffected()
				if err != nil {
					return nil, err
				}
				written += int(count)
				clock.Add(int64(2 * importGroupLease / time.Millisecond))
			}
			return result, nil
		},
	})
	before := creationEffectCounts(t, service.database)
	result, err := commitPreparedFixture(t.Context(), service, plan, &work)
	if err == nil || result != (Created{}) || written != 1 {
		t.Fatalf("expired creation lease published: result=%+v written=%d error=%v", result, written, err)
	}
	assertCreationEffectsUnchanged(t, service.database, before)
	var state string
	if err := service.database.QueryRowContext(t.Context(), `SELECT state FROM jobs WHERE id=?`, created.JobID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "RUNNING" {
		t.Fatalf("late failure changed job to %s", state)
	}
}
