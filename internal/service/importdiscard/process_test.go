package importdiscard

import (
	"context"
	"errors"
	"reflect"
	model "retrom/internal/model/importdiscard"
	"testing"
	"time"
)

type importCalls struct {
	calls       []string
	cancelErr   error
	reviewsDone bool
}

func (calls *importCalls) CancelForDiscard(context.Context, string, int64) error {
	calls.calls = append(calls.calls, "cancel")
	return calls.cancelErr
}

func (calls *importCalls) DiscardBatchReviews(context.Context, string) (bool, error) {
	calls.calls = append(calls.calls, "reviews")
	return calls.reviewsDone, nil
}

func (calls *importCalls) ReleaseDiscardedBatch(context.Context, string) error {
	calls.calls = append(calls.calls, "release")
	return nil
}

func TestDiscardStopsExecutionBeforeReleasingReviews(t *testing.T) {
	tests := []struct {
		state string
		done  bool
		want  []string
	}{
		{"RUNNING", false, []string{"cancel"}},
		{"CANCEL_REQUESTED", false, nil},
		{"CANCELLED", true, []string{"reviews", "release"}},
		{"FAILED", true, []string{"reviews", "release"}},
		{"COMPLETED", true, []string{"reviews", "release"}},
	}
	for _, test := range tests {
		t.Run(test.state, func(t *testing.T) {
			calls := &importCalls{reviewsDone: true}
			repository := &memoryRepository{records: &memoryRecords{batch: model.Batch{State: test.state, Version: 7}}}
			service := New(repository, calls, nil, func() time.Time { return time.UnixMilli(17) })
			done, err := service.discardImport(t.Context(), batchID)
			if err != nil || done != test.done || !reflect.DeepEqual(calls.calls, test.want) {
				t.Fatalf("done=%t err=%v calls=%v", done, err, calls.calls)
			}
		})
	}
}

func TestDiscardWaitsForReviewDrainAndPreservesCancellationFailure(t *testing.T) {
	records := &memoryRecords{batch: model.Batch{State: "COMPLETED"}}
	calls := &importCalls{}
	service := New(&memoryRepository{records: records}, calls, nil, func() time.Time { return time.UnixMilli(17) })
	if done, err := service.discardImport(t.Context(), batchID); err != nil || done || !reflect.DeepEqual(calls.calls, []string{"reviews"}) {
		t.Fatalf("released before review drain: done=%t err=%v calls=%v", done, err, calls.calls)
	}
	records.batch.State = "RUNNING"
	calls.cancelErr = model.ErrNotCancellable
	if done, err := service.discardImport(t.Context(), batchID); done || err != nil {
		t.Fatalf("concurrent stop: %t %v", done, err)
	}
	calls.cancelErr = context.Canceled
	if _, err := service.discardImport(t.Context(), batchID); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
}
