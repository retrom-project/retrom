package libraryimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/libraryimport"
)

type reviewDeduplicateRepositoryStub struct {
	result model.ReviewDeduplicateResult
	err    error
	cmd    model.DeduplicateCommand
	calls  int
}

func (repository *reviewDeduplicateRepositoryStub) CommitDeduplicate(
	_ context.Context, cmd model.DeduplicateCommand,
) (model.ReviewDeduplicateResult, error) {
	repository.calls++
	repository.cmd = cmd
	return repository.result, repository.err
}

func TestReviewDeduplicatorUsesFrozenPageAndDiscardsPublishedMatches(t *testing.T) {
	through := "01990000-0000-7000-8000-000000000099"
	repository := &reviewDeduplicateRepositoryStub{
		result: model.ReviewDeduplicateResult{
			ScannedCount:          2,
			DiscardedCount:        1,
			AttachmentActiveCount: 1,
			ThroughItemID:         &through,
		},
	}
	service := NewReviewDeduplicator(repository, func() time.Time { return time.UnixMilli(88) })

	result, err := service.Deduplicate(t.Context(), model.ReviewDeduplicateRequest{
		Scope: model.ReviewBulkScope{Q: "  Test   Game  "}, AfterItemID: "01990000-0000-7000-8000-000000000000",
		ThroughItemID: through,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.calls != 1 || result.ScannedCount != 2 || result.AttachmentActiveCount != 1 || result.DiscardedCount != 1 {
		t.Fatalf("calls/result = %d/%+v", repository.calls, result)
	}
	if result.ThroughItemID == nil || *result.ThroughItemID != through || result.NextAfterItemID != nil {
		t.Fatalf("cursor result = %+v", result)
	}
	cmd := repository.cmd
	if cmd.Request.Scope.Q != "test game" || cmd.Request.ThroughItemID != through {
		t.Fatalf("command request = %+v", cmd.Request)
	}
	if len(cmd.Discards) != reviewDeduplicatePageSize {
		t.Fatalf("discard slots = %d", len(cmd.Discards))
	}
	if cmd.NowMS == 0 {
		t.Fatal("NowMS not set")
	}
}

func TestReviewDeduplicatorPropagatesDiscardFailureWithoutResult(t *testing.T) {
	through := "01990000-0000-7000-8000-000000000099"
	want := errors.New("discard failed")
	repository := &reviewDeduplicateRepositoryStub{err: want}
	service := NewReviewDeduplicator(repository, func() time.Time { return time.UnixMilli(88) })

	result, err := service.Deduplicate(t.Context(), model.ReviewDeduplicateRequest{ThroughItemID: through})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
	if result != (model.ReviewDeduplicateResult{}) {
		t.Fatalf("result = %+v", result)
	}
}
