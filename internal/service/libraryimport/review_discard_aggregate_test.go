package libraryimport

import (
	"errors"
	"math"
	"testing"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/model/importprogress"
)

func TestReviewDiscardProjectsParentFromSameSnapshot(t *testing.T) {
	t.Parallel()
	fixture := newDiscardFixture()
	fixture.snapshot.Aggregate.Version = 9
	fixture.snapshot.Aggregate.Progress.Counts.ReviewPending = 2
	fixture.snapshot.Aggregate.Progress.Counts.Failed = 1
	result, err := discardService(fixture).Discard(t.Context(), discardRequest())
	if err != nil || result.Status != "DISCARDED" {
		t.Fatalf("discard=%+v err=%v", result, err)
	}
	change := fixture.change.Aggregate
	if change.ExpectedVersion != 9 || change.ExpectedPending != 2 ||
		change.Projection.State != "PARTIAL_FAILURE" || change.Projection.CompletedAtMS != nil {
		t.Fatalf("parent change lost snapshot or failure: %+v", change)
	}
	if fixture.snapshot.Aggregate.Progress.Counts.ReviewPending != 2 {
		t.Fatal("projection mutated original evidence")
	}
}

func TestReviewDiscardRejectsUnusableParentBeforeWrites(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*model.ReviewDiscardAggregate)
		cause  error
	}{
		{"missing version", func(a *model.ReviewDiscardAggregate) { a.Version = 0 }, model.ErrInvalid},
		{"overflow", func(a *model.ReviewDiscardAggregate) { a.Version = math.MaxInt64 }, model.ErrInvalid},
		{"no pending", func(a *model.ReviewDiscardAggregate) { a.Progress.Counts.ReviewPending = 0 }, model.ErrInvalid},
		{"invalid failed count", func(a *model.ReviewDiscardAggregate) { a.Progress.Counts.Failed = -1 }, importprogress.ErrInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newDiscardFixture()
			test.change(&fixture.snapshot.Aggregate)
			result, err := discardService(fixture).Discard(t.Context(), discardRequest())
			if !errors.Is(err, test.cause) || result != (model.ReviewDecisionResult{}) || len(fixture.steps) != 0 {
				t.Fatalf("invalid parent reached writes: result=%+v err=%v steps=%v", result, err, fixture.steps)
			}
		})
	}
}
