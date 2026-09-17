package emulationstationimport

import (
	"context"
	"errors"
	model "retrom/internal/model/emulationstationimport"
	"testing"
	"testing/synctest"
	"time"
)

func TestWorkerOwnerLossAndStorageErrorsStopWithoutSettlement(t *testing.T) {
	for _, kind := range []string{"lost", "read", "renew"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				worker, fixture := newWorkerFixture()
				cause := errors.New("worker storage failed")
				switch kind {
				case "lost":
					fixture.observe(model.LeaseLost)
				case "read":
					fixture.observeErr = cause
				case "renew":
					fixture.renewErr = cause
				}
				done := make(chan struct{})
				go func() {
					worker.Run(context.Background(), model.Execution{DeadlineAtMS: time.Now().Add(time.Hour).UnixMilli()})
					close(done)
				}()
				synctest.Wait()
				if kind == "renew" {
					time.Sleep(15 * time.Second)
					synctest.Wait()
				}
				<-done
				if fixture.acknowledged.Load() != 0 || fixture.failed.Load() != 0 {
					t.Fatal("unowned work was settled")
				}
				if kind != "lost" {
					assertWorkerReport(t, fixture, cause)
				}
			})
		})
	}
}

func TestWorkerDurableLeaseLossInterruptsExecutingWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		worker, fixture := newWorkerFixture()
		worker.Start()
		synctest.Wait()
		fixture.observe(model.LeaseLost)
		worker.Signal()
		synctest.Wait()
		worker.Close()
		if fixture.executed.Load() != 1 || fixture.acknowledged.Load() != 0 || fixture.failed.Load() != 0 {
			t.Fatal("replacement owner was changed")
		}
	})
}

func TestWorkerAcknowledgementFailureAndTimeoutRetainCause(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "storage", true: "timeout"}[timeout], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				worker, fixture := newWorkerFixture()
				fixture.observe(model.LeaseCancelled)
				cause := errors.New("acknowledgement commit failed")
				fixture.ackErr = cause
				if timeout {
					cause = context.DeadlineExceeded
					fixture.settle = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
				}
				before := time.Now()
				worker.Run(context.Background(), model.Execution{DeadlineAtMS: time.Now().Add(time.Hour).UnixMilli()})
				if fixture.executed.Load() != 0 || fixture.acknowledged.Load() != 1 || fixture.failed.Load() != 0 {
					t.Fatal("cancelled execution ran")
				}
				if timeout && time.Since(before) != 30*time.Second {
					t.Fatal("cleanup did not use bounded 30 second lifetime")
				}
				assertWorkerReport(t, fixture, cause)
			})
		})
	}
}

func TestWorkerOriginalBudgetUsesInjectedClockAndJoinsRenewal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		worker, fixture := newWorkerFixture()
		worker.now = func() time.Time { return time.UnixMilli(1000) }
		done := make(chan struct{})
		go func() { worker.Run(context.Background(), model.Execution{DeadlineAtMS: 18000}); close(done) }()
		synctest.Wait()
		time.Sleep(15 * time.Second)
		synctest.Wait()
		if fixture.renewals.Load() != 1 || fixture.failed.Load() != 0 {
			t.Fatal("original budget was expired or reset")
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		<-done
		if fixture.acknowledged.Load() != 0 || fixture.failed.Load() != 1 {
			t.Fatal("original timeout was not classified independently")
		}
		time.Sleep(30 * time.Second)
		if fixture.renewals.Load() != 1 {
			t.Fatal("renewal survived execution")
		}
	})
}

func TestWorkerParentDeadlineDoesNotBecomeExecutionFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		worker, fixture := newWorkerFixture()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		worker.Run(ctx, model.Execution{DeadlineAtMS: time.Now().Add(time.Hour).UnixMilli()})
		if fixture.acknowledged.Load() != 0 || fixture.failed.Load() != 0 {
			t.Fatal("parent deadline wrote execution outcome")
		}
	})
}

func TestWorkerExpiredAuthorityUsesRecoveryAndReportsFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		worker, fixture := newWorkerFixture()
		fixture.errorFailure = model.ErrExpired
		cause := errors.New("recovery transaction failed")
		fixture.maintain = func(context.Context) error { return cause }
		worker.Run(context.Background(), model.Execution{DeadlineAtMS: time.Now().UnixMilli()})
		if fixture.executed.Load() != 0 || fixture.failed.Load() != 1 || fixture.maintained.Load() != 1 {
			t.Fatal("expired execution bypassed recovery")
		}
		assertWorkerReport(t, fixture, cause)
	})
}

func assertWorkerReport(t *testing.T, fixture *workerFixture, cause error) {
	t.Helper()
	select {
	case err := <-fixture.reports:
		if !errors.Is(err, cause) {
			t.Fatalf("cause=%v", err)
		}
	default:
		t.Fatal("worker swallowed operation failure")
	}
}
