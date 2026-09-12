package pegasusimport

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestRecoveryLoopClosesLeaseThatExpiresAfterStartup(t *testing.T) {
	t.Parallel()
	service := recoveryFixture(t)
	mustExecPegasusTest(t.Context(), t, service.database, `UPDATE jobs SET leased_until_ms=20,execution_deadline_at_ms=25 WHERE id='work'`)
	var now atomic.Int64
	now.Store(10)
	service.now = func() time.Time { return time.UnixMilli(now.Load()) }
	service.wake = make(chan struct{})
	service.stop = make(chan struct{})
	done := make(chan struct{})
	go func() { defer close(done); service.runLoop() }()
	t.Cleanup(func() { service.Close(); <-done })
	// Two rendezvous ensure a complete maintenance cycle with a still-live lease.
	service.wake <- struct{}{}
	service.wake <- struct{}{}
	now.Store(30)
	service.wake <- struct{}{}
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
