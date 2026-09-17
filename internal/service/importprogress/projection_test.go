package importprogress

import (
	"errors"
	"testing"

	model "retrom/internal/model/importprogress"
)

func TestProjectImportStatePriority(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		snapshot  model.Snapshot
		state     string
		completed bool
	}{
		{"not claimed", model.Snapshot{Counts: model.Counts{Queued: 1}}, "QUEUED", false},
		{"queued after started", model.Snapshot{Started: true, Counts: model.Counts{Queued: 1}}, "RUNNING", false},
		{"running before failure", model.Snapshot{Started: true, Counts: model.Counts{Running: 1, Failed: 1}}, "RUNNING", false},
		{"failure before review", model.Snapshot{Started: true, Counts: model.Counts{Failed: 1, ReviewPending: 1}}, "PARTIAL_FAILURE", false},
		{"unresolved rejected", model.Snapshot{Started: true, Counts: model.Counts{Rejected: 2, ResolvedRejected: 1}}, "PARTIAL_FAILURE", false},
		{"only review", model.Snapshot{Started: true, Counts: model.Counts{ReviewPending: 1}}, "REVIEW_PENDING", false},
		{"all resolved", model.Snapshot{Started: true, Counts: model.Counts{Rejected: 2, ResolvedRejected: 2}}, "COMPLETED", true},
		{"all decisions finished", model.Snapshot{Started: true}, "COMPLETED", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result, err := model.Project(test.snapshot, 88)
			if err != nil || result.State != test.state || (result.CompletedAtMS != nil) != test.completed {
				t.Fatalf("aggregate=%+v err=%v want=%s/%v", result, err, test.state, test.completed)
			}
			if result.CompletedAtMS != nil && *result.CompletedAtMS != 88 {
				t.Fatalf("completion time=%d", *result.CompletedAtMS)
			}
		})
	}
}

func TestProjectPreservesCancellationAndTaskFailure(t *testing.T) {
	t.Parallel()
	requested, completed := int64(30), int64(40)
	for _, test := range []model.Snapshot{
		{State: "CANCEL_REQUESTED", Started: true, Counts: model.Counts{Running: 1, Failed: 1}, CancelRequestedAtMS: &requested},
		{State: "CANCELLED", Started: true, Counts: model.Counts{Failed: 1, Cancelled: 1}, CancelRequestedAtMS: &requested, CompletedAtMS: &completed},
		{State: "FAILED", Started: true, Counts: model.Counts{ReviewPending: 1}, CompletedAtMS: &completed},
	} {
		t.Run(test.State, func(t *testing.T) {
			t.Parallel()
			result, err := model.Project(test, 88)
			if err != nil || result.State != test.State || result.CompletedAtMS != test.CompletedAtMS {
				t.Fatalf("terminal lifecycle changed: %+v err=%v", result, err)
			}
		})
	}
}

func TestProjectRejectsInvalidCounts(t *testing.T) {
	t.Parallel()
	for _, counts := range []model.Counts{{Queued: -1}, {Running: -1}, {ReviewPending: -1}, {Failed: -1}, {Cancelled: -1}, {Rejected: -1}, {ResolvedRejected: -1}, {Rejected: 1, ResolvedRejected: 2}, {Cancelled: 1}} {
		result, err := model.Project(model.Snapshot{Started: true, Counts: counts}, 88)
		if !errors.Is(err, model.ErrInvalid) || result != (model.Projection{}) {
			t.Fatalf("invalid aggregate accepted: %+v result=%+v err=%v", counts, result, err)
		}
	}
}
