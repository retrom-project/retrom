package libraryimport

import (
	"context"
	"testing"
	"time"
)

type replayBulkStore struct {
	ReviewBulkStore
	worker replayBulkWorker
}

func (store *replayBulkStore) WithStep(_ context.Context, run func(ReviewBulkStep) error) error {
	scope := ReviewBulkStep{
		Worker:   &store.worker,
		Approval: ReviewApprovalScope{Publications: replayBulkPublication{}},
	}
	// The first attempt observes a publication, then loses its snapshot.
	// In the committed retry another worker has already drained the queue.
	if err := run(scope); err != nil {
		return err
	}
	return run(scope)
}

type replayBulkWorker struct {
	ReviewBulkWorker
	reads, finishes int
}

func (worker *replayBulkWorker) Next(context.Context, string, string) (ReviewBulkScanItem, bool, error) {
	worker.reads++
	return ReviewBulkScanItem{ID: "item", ReviewVersion: 7}, worker.reads == 1, nil
}

func (worker *replayBulkWorker) Finish(context.Context, string, string, string, int64) error {
	worker.finishes++
	return nil
}

type replayBulkPublication struct{ PublicationRecords }

func (replayBulkPublication) ReadPublication(context.Context, string) (PublicationState, error) {
	return PublicationState{Found: true, GameID: "game"}, nil
}

func TestBulkRetryDiscardsPublicationFromRolledBackAttempt(t *testing.T) {
	store := &replayBulkStore{}
	service := NewReviewBulk(store, nil, func() time.Time { return time.UnixMilli(10) })
	t.Cleanup(service.Close)
	completed, err := service.processNextReviewBulkItem(t.Context(), reviewBulkWork{
		bulkID: "bulk", jobID: "job", workerID: "worker", userID: "actor",
	})
	if err != nil || !completed || store.worker.reads != 2 || store.worker.finishes != 1 {
		t.Fatalf("completed=%v error=%v reads=%d finishes=%d",
			completed, err, store.worker.reads, store.worker.finishes)
	}
}
