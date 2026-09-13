package pegasusimport

import (
	"context"
	"database/sql/driver"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"retrom/internal/testkit/testsupport"
)

func TestRecoveryLoopClosesLeaseThatExpiresAfterStartup(t *testing.T) {
	t.Parallel()
	service := recoveryFixture(t)
	mustExecPegasusTest(t.Context(), t, service.database, `UPDATE jobs SET leased_until_ms=20,execution_deadline_at_ms=25 WHERE id='work'`)
	var now atomic.Int64
	now.Store(10)
	service.now = func() time.Time { return time.UnixMilli(now.Load()) }
	observed := make(chan struct{})
	var first sync.Once
	service.database = testsupport.OpenSQLFaultDatabase(t, service.database, testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
			if strings.Contains(query, "ORDER BY job.leased_until_ms,job.id LIMIT ?") && len(args) == 3 {
				first.Do(func() { close(observed) })
			}
			return nil
		},
	})
	service.Start()
	t.Cleanup(service.Close)
	select {
	case <-observed:
	case <-time.After(2 * time.Second):
		t.Fatal("initial maintenance never ran")
	}
	now.Store(30)
	service.signal()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var state string
		if err := service.database.QueryRowContext(t.Context(), `SELECT state FROM jobs WHERE id='work'`).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "FAILED" {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("expired lease never recovered after startup: %s", state)
		case <-tick.C:
		}
	}
}
