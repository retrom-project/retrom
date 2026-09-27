package libraryimport

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type bulkLifecycleStore struct {
	ReviewBulkStore
	worker *bulkLifecycleWorker
}

func (store *bulkLifecycleStore) WithStep(_ context.Context, run func(ReviewBulkStep) error) error {
	return run(ReviewBulkStep{Worker: store.worker, Recovery: store.worker})
}

type bulkLifecycleWorker struct {
	ReviewBulkWorker
	resumes atomic.Int32
	started chan struct{}
	ended   chan struct{}
}

func (worker *bulkLifecycleWorker) Resume(context.Context, int64) ([]string, error) {
	worker.resumes.Add(1)
	return []string{"bulk"}, nil
}

func (worker *bulkLifecycleWorker) Claim(ctx context.Context, _, _ string, _ int64) (string, string, error) {
	close(worker.started)
	<-ctx.Done()
	close(worker.ended)
	return "", "", ctx.Err()
}

func TestBulkStartupIsIdempotentAndCloseJoinsWorkers(t *testing.T) {
	worker := &bulkLifecycleWorker{started: make(chan struct{}), ended: make(chan struct{})}
	service := NewReviewBulk(&bulkLifecycleStore{worker: worker}, nil, func() time.Time { return time.UnixMilli(10) })
	t.Cleanup(service.Close)
	if err := service.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	<-worker.started
	if err := service.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	service.Close()
	select {
	case <-worker.ended:
	default:
		t.Fatal("Close returned before worker cancellation completed")
	}
	if err := service.Start(t.Context()); !errors.Is(err, ErrReviewBulkConflict) || worker.resumes.Load() != 1 {
		t.Fatalf("closed startup=%v recoveries=%d", err, worker.resumes.Load())
	}
}

func TestAutomaticApprovalLeavesManualDecisionsToTheReviewer(t *testing.T) {
	validation, ready := "validation", "READY"
	candidate := ReviewBulkCandidate{ValidationID: &validation, ValidationStatus: &ready}
	if !automaticApprovalCandidate(candidate) {
		t.Fatal("READY candidate was excluded")
	}
	for _, change := range []func(*ReviewBulkCandidate){
		func(value *ReviewBulkCandidate) { value.ValidationID = nil },
		func(value *ReviewBulkCandidate) { blocked := "BLOCKED"; value.ValidationStatus = &blocked },
		func(value *ReviewBulkCandidate) { value.AttachmentActive = true },
		func(value *ReviewBulkCandidate) { value.SourceFlagged = true },
	} {
		value := candidate
		change(&value)
		if automaticApprovalCandidate(value) {
			t.Fatalf("manual decision entered automatic approval: %#v", value)
		}
	}
}
