package pegasusimport

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type workerFixture struct {
	claimCount, renewCount, maintainCount, executeCount, closeCount atomic.Int64
	cancelRequested                                                 atomic.Bool
	ownerLost                                                       atomic.Bool
	renewError, closeError                                          error
	checkError                                                      error
	run                                                             func(context.Context, Work)
	maintain                                                        func(context.Context) error
	settle                                                          func(context.Context) error
	reports                                                         chan error
}

func (f *workerFixture) Claim(context.Context) (Work, bool, error) {
	count := f.claimCount.Add(1)
	return Work{JobID: "job", ImportID: "plan", WorkerID: "owner", ExecutionNo: 1, Attempt: 1}, count == 1, nil
}

func (f *workerFixture) Renew(context.Context, ExecutionIdentity) error {
	f.renewCount.Add(1)
	return f.renewError
}

func (f *workerFixture) Maintain(ctx context.Context) error {
	f.maintainCount.Add(1)
	if f.maintain != nil {
		return f.maintain(ctx)
	}
	return nil
}

func (f *workerFixture) Execute(ctx context.Context, unit Work) {
	f.executeCount.Add(1)
	f.run(ctx, unit)
}

func (f *workerFixture) Cancelled(context.Context, ExecutionIdentity) (bool, error) {
	if f.ownerLost.Load() {
		return false, ErrVersionConflict
	}
	return f.cancelRequested.Load(), f.checkError
}

func (f *workerFixture) CloseCancelled(ctx context.Context, _ ExecutionIdentity) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	f.closeCount.Add(1)
	if f.settle != nil {
		return false, f.settle(ctx)
	}
	return true, f.closeError
}

func newWorkerFixture() (*Worker, *workerFixture) {
	f := &workerFixture{reports: make(chan error, 20)}
	f.run = func(ctx context.Context, _ Work) { <-ctx.Done() }
	worker := NewWorker(WorkerDependencies{
		Leases: f, Maintenance: f, Executor: f, Cancellation: f,
		Report: func(err error) { f.reports <- err },
	})
	return worker, f
}

func TestWorkerStartIsIdempotentAndCloseJoinsExecution(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		worker, f := newWorkerFixture()
		release := make(chan struct{})
		f.run = func(ctx context.Context, _ Work) { <-ctx.Done(); <-release }
		worker.Start()
		worker.Start()
		synctest.Wait()
		if f.executeCount.Load() != 1 || f.claimCount.Load() != 1 {
			t.Fatal("duplicate worker start")
		}
		closed := make(chan struct{})
		go func() { worker.Close(); close(closed) }()
		synctest.Wait()
		select {
		case <-closed:
			t.Fatal("Close returned before execution cleanup")
		default:
		}
		close(release)
		synctest.Wait()
		<-closed
		before := f.maintainCount.Load()
		worker.Start()
		worker.Signal()
		worker.Close()
		time.Sleep(2 * time.Second)
		if f.executeCount.Load() != 1 || f.maintainCount.Load() != before {
			t.Fatal("closed worker restarted")
		}
	})
}

func TestWorkerMaintainsExpiredLeasesWhileExecutionIsBlocked(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		worker, f := newWorkerFixture()
		worker.Start()
		synctest.Wait()
		initial := f.maintainCount.Load()
		time.Sleep(3 * time.Second)
		synctest.Wait()
		if f.maintainCount.Load() <= initial || f.executeCount.Load() != 1 {
			t.Fatal("execution blocked maintenance")
		}
		worker.Close()
	})
}

func TestWorkerObservesDurableCancellationWithSignalOrPolling(t *testing.T) {
	for _, signal := range []bool{false, true} {
		name := "poll"
		if signal {
			name = "signal"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				worker, f := newWorkerFixture()
				worker.Start()
				synctest.Wait()
				f.cancelRequested.Store(true)
				if signal {
					worker.Signal()
				} else {
					time.Sleep(time.Second)
				}
				synctest.Wait()
				if f.closeCount.Load() != 1 || f.renewCount.Load() != 0 {
					t.Fatal("cancelled work remained active or renewed")
				}
				worker.Close()
			})
		})
	}
}

func TestWorkerOwnerReadFailurePreservesCauseAndStopsWithoutSettlement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		worker, f := newWorkerFixture()
		cause := errors.New("owner unavailable")
		f.checkError = cause
		worker.Start()
		synctest.Wait()
		worker.Close()
		if f.executeCount.Load() != 0 || f.closeCount.Load() != 0 {
			t.Fatal("unverified owner executed or settled")
		}
		select {
		case err := <-f.reports:
			if !errors.Is(err, cause) {
				t.Fatalf("lost cause: %v", err)
			}
		default:
			t.Fatal("owner error was swallowed")
		}
	})
}

func TestWorkerCloseCancelsAndJoinsMaintenance(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		worker, f := newWorkerFixture()
		exited := make(chan struct{})
		f.maintain = func(ctx context.Context) error { <-ctx.Done(); close(exited); return ctx.Err() }
		worker.Start()
		synctest.Wait()
		worker.Close()
		select {
		case <-exited:
		default:
			t.Fatal("maintenance survived Close")
		}
	})
}

func TestWorkerDeadlineAndHeartbeatUseOriginalExecutionLifetime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		worker, f := newWorkerFixture()
		done := make(chan struct{})
		go func() {
			worker.Run(context.Background(), Work{DeadlineAtMS: time.Now().Add(17 * time.Second).UnixMilli()})
			close(done)
		}()
		synctest.Wait()
		time.Sleep(15 * time.Second)
		synctest.Wait()
		if f.renewCount.Load() != 1 {
			t.Fatalf("renewals=%d", f.renewCount.Load())
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		<-done
		if f.closeCount.Load() != 0 {
			t.Fatal("deadline guessed cancellation authority")
		}
		time.Sleep(30 * time.Second)
		if f.renewCount.Load() != 1 {
			t.Fatal("heartbeat survived execution")
		}
	})
}

func TestWorkerReplacementStopsIOWithoutClosingReplacement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		worker, f := newWorkerFixture()
		worker.Start()
		synctest.Wait()
		f.ownerLost.Store(true)
		worker.Signal()
		synctest.Wait()
		worker.Close()
		if f.executeCount.Load() != 1 || f.closeCount.Load() != 0 || f.renewCount.Load() != 0 {
			t.Fatal("replacement owner was written or old IO survived")
		}
	})
}

func TestWorkerLeaseFailureStopsAndJoinsExecution(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		worker, f := newWorkerFixture()
		cause := errors.New("lease write failed")
		f.renewError = cause
		done := make(chan struct{})
		go func() { worker.Run(context.Background(), Work{}); close(done) }()
		synctest.Wait()
		time.Sleep(15 * time.Second)
		synctest.Wait()
		<-done
		if f.closeCount.Load() != 0 {
			t.Fatal("lease failure guessed settlement authority")
		}
		select {
		case err := <-f.reports:
			if !errors.Is(err, cause) {
				t.Fatalf("lost cause: %v", err)
			}
		default:
			t.Fatal("renewal failure was swallowed")
		}
	})
}

func TestWorkerCancellationSettlementFailureIsReported(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		worker, f := newWorkerFixture()
		cause := errors.New("cancellation commit failed")
		f.closeError = cause
		f.cancelRequested.Store(true)
		worker.Run(context.Background(), Work{})
		if f.executeCount.Load() != 0 || f.closeCount.Load() != 1 {
			t.Fatal("already cancelled execution ran")
		}
		select {
		case err := <-f.reports:
			if !errors.Is(err, cause) {
				t.Fatalf("lost cause: %v", err)
			}
		default:
			t.Fatal("failed cancellation settlement was swallowed")
		}
	})
}

func TestWorkerCancellationCleanupTimeoutIsReported(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		worker, f := newWorkerFixture()
		f.cancelRequested.Store(true)
		f.settle = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
		worker.Run(context.Background(), Work{})
		select {
		case err := <-f.reports:
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("lost cleanup timeout: %v", err)
			}
		default:
			t.Fatal("bounded cleanup timeout was swallowed")
		}
	})
}
