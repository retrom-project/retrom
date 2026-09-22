package libraryimport

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"retrom/internal/service/importprogress"
)

type approvalHeadStub struct {
	ReviewApprovalReader
	head  ReviewApprovalHead
	found bool
	cause error
}

func (stub approvalHeadStub) Head(context.Context, string) (ReviewApprovalHead, bool, error) {
	return stub.head, stub.found, stub.cause
}

type approvalScopeStub struct {
	scope ReviewApprovalScope
	calls int
}

func (stub *approvalScopeStub) WithApproval(_ context.Context, work func(ReviewApprovalScope) error) error {
	stub.calls++
	return work(stub.scope)
}

func TestReviewApprovalRejectsAuthorityBeforeReadingChildren(t *testing.T) {
	valid := ReviewApprovalHead{State: "REVIEW_PENDING", DraftVersion: 1, ValidationStatus: "READY", ValidationID: "validation", SourceSnapshotID: "snapshot"}
	for _, name := range []string{"missing", "wrong state", "version", "source busy", "bulk status", "bulk validation", "bulk snapshot"} {
		t.Run(name, func(t *testing.T) {
			head, found := valid, true
			request := ReviewApprovalRequest{ItemID: "item", ExpectedVersion: 1}
			switch name {
			case "missing":
				found = false
			case "wrong state":
				head.State = "PUBLISHED"
			case "version":
				head.DraftVersion = 2
			case "source busy":
				head.SourceBusy = true
			default:
				request.Bulk = &BulkPublicationIntent{BulkID: "bulk", JobID: "job", WorkerID: "worker", ValidationID: "validation", SourceSnapshotID: "snapshot"}
				switch name {
				case "bulk status":
					head.ValidationStatus = "BLOCKED"
				case "bulk validation":
					head.ValidationID = "changed"
				case "bulk snapshot":
					head.SourceSnapshotID = "changed"
				}
			}
			repo := &approvalScopeStub{scope: ReviewApprovalScope{Reader: approvalHeadStub{head: head, found: found}}}
			result, err := NewReviewApprovals(repo, nil, nil).Approve(t.Context(), request)
			if !errors.Is(err, ErrInvalid) || result != (ReviewApproved{}) || repo.calls != 1 {
				t.Fatalf("result=%+v err=%v calls=%d", result, err, repo.calls)
			}
		})
	}
}

func TestReviewApprovalPreservesReadAndContextErrors(t *testing.T) {
	for _, cause := range []error{errors.New("repository unavailable"), context.Canceled} {
		repo := &approvalScopeStub{scope: ReviewApprovalScope{Reader: approvalHeadStub{cause: cause}}}
		result, err := NewReviewApprovals(repo, nil, nil).Approve(t.Context(), ReviewApprovalRequest{ItemID: "item", ExpectedVersion: 1})
		if !errors.Is(err, cause) || errors.Is(err, ErrInvalid) || result != (ReviewApproved{}) {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
}

func TestReviewApprovalAllocatesEveryIdentityBeforePublication(t *testing.T) {
	cause := errors.New("entropy unavailable")
	for failAt := 1; failAt <= 4; failAt++ {
		calls := 0
		service := NewReviewApprovals(nil, nil, nil)
		service.newID = func() (string, error) {
			calls++
			if calls == failAt {
				return "", cause
			}
			return "identity", nil
		}
		run := reviewApprovalRun{service: service, assets: make([]ApprovalAsset, 2)}
		if err := run.allocateIDs(); !errors.Is(err, cause) || calls != failAt {
			t.Fatalf("allocation %d: calls=%d err=%v", failAt, calls, err)
		}
	}
}

func TestReviewApprovalProjectsSharedProgressWithoutMutatingSnapshot(t *testing.T) {
	for _, test := range []struct {
		name, want string
		counts     importprogress.Counts
	}{
		{"completed", "COMPLETED", importprogress.Counts{ReviewPending: 1}},
		{"pending", "REVIEW_PENDING", importprogress.Counts{ReviewPending: 2}},
		{"failed", "PARTIAL_FAILURE", importprogress.Counts{ReviewPending: 1, Failed: 1}},
		{"running", "RUNNING", importprogress.Counts{ReviewPending: 1, Running: 1}},
		{"queued", "RUNNING", importprogress.Counts{ReviewPending: 1, Queued: 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := reviewApprovalRun{now: 123, head: ReviewApprovalHead{ParentVersion: 7, Progress: importprogress.Snapshot{State: "REVIEW_PENDING", Counts: test.counts}}}
			if err := run.projectAggregate(); err != nil {
				t.Fatal(err)
			}
			if run.publication.Projection.State != test.want || run.head.Progress.Counts != test.counts || run.publication.ExpectedParentVersion != 7 || run.publication.ExpectedPending != test.counts.ReviewPending {
				t.Fatalf("projection=%+v head=%+v", run.publication, run.head.Progress)
			}
		})
	}
}

func TestReviewApprovalRejectsInvalidParentBeforePublication(t *testing.T) {
	for _, head := range []ReviewApprovalHead{
		{ParentVersion: 0, Progress: importprogress.Snapshot{Counts: importprogress.Counts{ReviewPending: 1}}},
		{ParentVersion: math.MaxInt64, Progress: importprogress.Snapshot{Counts: importprogress.Counts{ReviewPending: 1}}},
		{ParentVersion: 1},
	} {
		run := reviewApprovalRun{head: head, now: time.Unix(1, 0).UnixMilli()}
		if err := run.projectAggregate(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("head=%+v err=%v", head, err)
		}
	}
}
