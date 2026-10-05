package sourceimport

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type typedWorkerFixture struct {
	*workerFixture
	scan, receive atomic.Int64
}

func (f *typedWorkerFixture) Claim(_ context.Context, kind string) (Work, bool, error) {
	counter := &f.scan
	if kind == "IMPORT_RECEIVE" {
		counter = &f.receive
	}
	id := counter.Add(1)
	return Work{Kind: kind, JobID: fmt.Sprintf("%s-%d", kind, id), ImportID: "plan", WorkerID: "owner", ExecutionNo: 1, Attempt: 1}, true, nil
}

func TestWorkerConcurrencyIsIndependentForEachJobKind(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, base := newWorkerFixture()
		fixture := &typedWorkerFixture{workerFixture: base}
		scanRelease := make(chan struct{}, 1)
		base.run = func(ctx context.Context, work Work) {
			if work.Kind == "IMPORT_SCAN" {
				select {
				case <-ctx.Done():
				case <-scanRelease:
				}
			} else {
				<-ctx.Done()
			}
		}
		worker := NewWorker(WorkerDependencies{Concurrency: map[string]int{"IMPORT_SCAN": 2, "IMPORT_RECEIVE": 1}, Leases: fixture, Maintenance: fixture, Executor: fixture, Cancellation: fixture})
		worker.Start()
		synctest.Wait()
		if fixture.scan.Load() != 2 || fixture.receive.Load() != 1 {
			t.Fatalf("initial pools scan=%d receive=%d", fixture.scan.Load(), fixture.receive.Load())
		}
		time.Sleep(3 * time.Second)
		synctest.Wait()
		if fixture.scan.Load() != 2 || fixture.receive.Load() != 1 {
			t.Fatal("busy workers claimed excess tasks")
		}
		scanRelease <- struct{}{}
		synctest.Wait()
		if fixture.scan.Load() != 3 || fixture.receive.Load() != 1 {
			t.Fatal("scan completion changed the receive pool")
		}
		worker.Close()
	})
}
