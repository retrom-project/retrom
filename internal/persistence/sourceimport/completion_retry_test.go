package sourceimport

import (
	"context"
	"database/sql/driver"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/sourceimport"
	"retrom/internal/testsupport"
)

func TestCompletionRetriesConcurrentHeartbeatWithoutDuplicateEvents(t *testing.T) {
	database := completionDatabase(t)
	database.SetMaxOpenConns(4)
	var conflicted atomic.Bool
	faultDatabase := testsupport.OpenSQLFaultDatabase(t, database, testsupport.SQLFaultHooks{
		BeforeExec: func(ctx context.Context, query string, _ []driver.NamedValue) error {
			if !strings.Contains(query, "UPDATE jobs SET state='SUCCEEDED'") || !conflicted.CompareAndSwap(false, true) {
				return nil
			}
			_, err := database.ExecContext(ctx, "UPDATE jobs SET heartbeat_at_ms=9,version=version+1 WHERE id='work'")
			return err
		},
	})
	service := application.NewCompletion(NewCompletion(faultDatabase), func() time.Time { return time.UnixMilli(10) })
	identity := application.ExecutionIdentity{JobID: "work", ImportID: "import-0", WorkerID: "old-worker", ExecutionNo: 1, Attempt: 1}
	if err := service.Finish(t.Context(), identity); err != nil {
		t.Fatal(err)
	}
	var state string
	var events int
	err := dbapi.QueryRowContext(t.Context(), database, `SELECT state,
(SELECT count(*) FROM job_events WHERE job_id='work' AND event_type='SUCCEEDED')
FROM jobs WHERE id='work'`).Scan(&state, &events)
	if err != nil || !conflicted.Load() || state != "SUCCEEDED" || events != 1 {
		t.Fatalf("conflicted=%v state=%s events=%d error=%v", conflicted.Load(), state, events, err)
	}
}
