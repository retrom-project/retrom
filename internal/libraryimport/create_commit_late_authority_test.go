//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
	"retrom/internal/testsupport"
)

func TestImportCreationRollsBackWhenLeaseExpiresAfterSourceWrite(t *testing.T) {
	service, plan := preparedCommitFixture(t)
	var clock atomic.Int64
	clock.Store(time.Now().UnixMilli())
	service = newTestImporter(t, service.database, service.blobs, testImportOptions{Now: func() time.Time { return time.UnixMilli(clock.Load()) }, MultiDiscEnabled: service.multiDiscImportEnabled})
	admissions := libraryservice.NewImportAdmissions(repository.NewImportAdmissions(service.database), nil, service.tags,
		libraryservice.ImportAdmissionOptions{Now: service.now})
	created, err := admissions.Queue(t.Context(), plan.Request)
	if err != nil {
		t.Fatal(err)
	}
	work, err := service.claimImportGroup(t.Context(), created.JobID)
	if err != nil {
		t.Fatal(err)
	}
	written := 0
	service = newTestImporter(t, testsupport.OpenSQLFaultDatabase(t, service.database, testsupport.SQLFaultHooks{
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
	}), service.blobs, testImportOptions{Now: service.now, MultiDiscEnabled: service.multiDiscImportEnabled})
	before := creationEffectCounts(t, service.database)
	result, err := commitPreparedFixture(t.Context(), service, plan, &work)
	if err == nil || result != (Created{}) || written != 1 {
		t.Fatalf("expired creation lease published: result=%+v written=%d error=%v", result, written, err)
	}
	assertCreationEffectsUnchanged(t, service.database, before)
	var state string
	if err := dbapi.QueryRowContext(t.Context(), service.database, `SELECT state FROM jobs WHERE id=?`, created.JobID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "RUNNING" {
		t.Fatalf("late failure changed job to %s", state)
	}
}
