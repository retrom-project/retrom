package emulationstationimport

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	model "retrom/internal/model/emulationstationimport"
)

func TestWorkerStartOnceAndCloseJoinsExecution(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		worker, fixture := newWorkerFixture()
		release := make(chan struct{})
		fixture.run = func(ctx context.Context, _ model.Execution) { <-ctx.Done(); <-release }
		worker.Start()
		worker.Start()
		synctest.Wait()
		if fixture.claims.Load() != 1 || fixture.executed.Load() != 1 {
			t.Fatal("duplicate execution")
		}
		closed := make(chan struct{})
		go func() { worker.Close(); close(closed) }()
		synctest.Wait()
		select {
		case <-closed:
			t.Fatal("Close did not join executor cleanup")
		default:
		}
		close(release)
		synctest.Wait()
		<-closed
		before := fixture.maintained.Load()
		worker.Start()
		worker.Signal()
		worker.Close()
		time.Sleep(2 * time.Second)
		if fixture.executed.Load() != 1 || fixture.maintained.Load() != before {
			t.Fatal("closed worker restarted")
		}
		if fixture.acknowledged.Load() != 0 || fixture.failed.Load() != 0 {
			t.Fatal("shutdown settled execution")
		}
	})
}

func TestWorkerMaintainsIndependentlyAndJoinsMaintenance(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		worker, fixture := newWorkerFixture()
		worker.Start()
		synctest.Wait()
		before := fixture.maintained.Load()
		time.Sleep(3 * time.Second)
		synctest.Wait()
		if fixture.maintained.Load() <= before || fixture.executed.Load() != 1 {
			t.Fatal("executor blocked maintenance")
		}
		worker.Close()
		worker, fixture = newWorkerFixture()
		exited := make(chan struct{})
		fixture.maintain = func(ctx context.Context) error { <-ctx.Done(); close(exited); return ctx.Err() }
		worker.Start()
		synctest.Wait()
		worker.Close()
		select {
		case <-exited:
		default:
			t.Fatal("Close did not join maintenance")
		}
	})
}

func TestWorkerCancellationSignalAndDurablePolling(t *testing.T) {
	for _, signal := range []bool{false, true} {
		t.Run(map[bool]string{false: "poll", true: "signal"}[signal], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				worker, fixture := newWorkerFixture()
				worker.Start()
				synctest.Wait()
				fixture.observe(model.LeaseCancelled)
				if signal {
					worker.Signal()
				} else {
					time.Sleep(time.Second)
				}
				synctest.Wait()
				if fixture.acknowledged.Load() != 1 || fixture.failed.Load() != 0 {
					t.Fatal("cancellation did not acknowledge exactly once")
				}
				worker.Close()
			})
		})
	}
}

func TestWorkerObservesCancellationAtExecutorReturn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		worker, fixture := newWorkerFixture()
		fixture.run = func(context.Context, model.Execution) { fixture.observe(model.LeaseCancelled) }
		worker.Run(context.Background(), model.Execution{DeadlineAtMS: time.Now().Add(time.Hour).UnixMilli()})
		if fixture.acknowledged.Load() != 1 {
			t.Fatal("executor return lost cancellation before monitor tick")
		}
	})
}
