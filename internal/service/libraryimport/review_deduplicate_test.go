package libraryimport

import (
	"context"
	"errors"
	model "retrom/internal/model/libraryimport"
	"testing"
)

type reviewDeduplicateRepositoryStub struct {
	scope model.ReviewDeduplicateScope
	calls int
}

func (repository *reviewDeduplicateRepositoryStub) WithDeduplicate(
	_ context.Context, work func(model.ReviewDeduplicateScope) error,
) error {
	repository.calls++
	return work(repository.scope)
}

type reviewDeduplicateReaderStub struct {
	through    *string
	candidates []model.ReviewBulkCandidate
	query      model.ReviewBulkCandidateQuery
}

func (reader *reviewDeduplicateReaderStub) LatestReviewItemID(context.Context) (*string, error) {
	return reader.through, nil
}

func (reader *reviewDeduplicateReaderStub) Candidates(
	_ context.Context, query model.ReviewBulkCandidateQuery,
) ([]model.ReviewBulkCandidate, error) {
	reader.query = query
	return reader.candidates, nil
}

type reviewDeduplicateDuplicatesStub struct {
	gamesByItem map[string][]model.DuplicateGame
	matched     []string
}

func (reader *reviewDeduplicateDuplicatesStub) Snapshot(
	_ context.Context, itemID string,
) (model.ContentSnapshot, error) {
	return model.ContentSnapshot{ID: itemID, Kind: "SINGLE_FILE"}, nil
}

func (*reviewDeduplicateDuplicatesStub) IdentityParts(context.Context, string) ([]model.ContentIdentityPart, error) {
	return []model.ContentIdentityPart{{Role: "ROM", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Count: 1}}, nil
}

func (*reviewDeduplicateDuplicatesStub) OrderedDiscs(context.Context, string) ([]model.ContentIdentityDisc, error) {
	return nil, nil
}

func (reader *reviewDeduplicateDuplicatesStub) PublishedMatches(
	_ context.Context, query model.DuplicateQuery,
) ([]model.DuplicateGame, error) {
	reader.matched = append(reader.matched, query.SnapshotID)
	return reader.gamesByItem[query.SnapshotID], nil
}

func (*reviewDeduplicateDuplicatesStub) ReviewPlatform(context.Context, string) (string, error) {
	return "gba", nil
}

type reviewDeduplicateDiscarderStub struct {
	requests []model.ReviewDiscardRequest
	err      error
}

func (discarder *reviewDeduplicateDiscarderStub) DiscardInScope(
	_ context.Context, _ model.ReviewDiscardScope, request model.ReviewDiscardRequest,
) (model.ReviewDecisionResult, error) {
	discarder.requests = append(discarder.requests, request)
	if discarder.err != nil {
		return model.ReviewDecisionResult{}, discarder.err
	}
	return model.ReviewDecisionResult{ItemID: request.ItemID, Status: "DISCARDED"}, nil
}

func TestReviewDeduplicatorUsesFrozenPageAndDiscardsPublishedMatches(t *testing.T) {
	through := "01990000-0000-7000-8000-000000000099"
	reader := &reviewDeduplicateReaderStub{
		through: &through,
		candidates: []model.ReviewBulkCandidate{
			{ItemID: "01990000-0000-7000-8000-000000000001", PlatformID: "gba", ReviewVersion: 3},
			{ItemID: "01990000-0000-7000-8000-000000000002", PlatformID: "gba", ReviewVersion: 4, AttachmentActive: true},
		},
	}
	duplicates := &reviewDeduplicateDuplicatesStub{gamesByItem: map[string][]model.DuplicateGame{
		"01990000-0000-7000-8000-000000000001": {{GameID: "published"}},
	}}
	discarder := &reviewDeduplicateDiscarderStub{}
	repository := &reviewDeduplicateRepositoryStub{scope: model.ReviewDeduplicateScope{
		Reader: reader, Duplicates: duplicates, Discard: model.ReviewDiscardScope{},
	}}
	service := NewReviewDeduplicator(repository, nil)
	service.discarder = discarder

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
	if reader.query.Scope.Q != "test game" || reader.query.Limit != reviewDeduplicatePageSize+1 {
		t.Fatalf("query = %+v", reader.query)
	}
	if len(duplicates.matched) != 1 || len(discarder.requests) != 1 || discarder.requests[0].ItemID != reader.candidates[0].ItemID {
		t.Fatalf("matches/discards = %#v/%#v", duplicates.matched, discarder.requests)
	}
}

func TestReviewDeduplicatorPropagatesDiscardFailureWithoutResult(t *testing.T) {
	through := "01990000-0000-7000-8000-000000000099"
	want := errors.New("discard failed")
	discarder := &reviewDeduplicateDiscarderStub{err: want}
	repository := &reviewDeduplicateRepositoryStub{scope: model.ReviewDeduplicateScope{
		Reader: &reviewDeduplicateReaderStub{through: &through, candidates: []model.ReviewBulkCandidate{{
			ItemID: "01990000-0000-7000-8000-000000000001", PlatformID: "gba", ReviewVersion: 1,
		}}},
		Duplicates: &reviewDeduplicateDuplicatesStub{gamesByItem: map[string][]model.DuplicateGame{
			"01990000-0000-7000-8000-000000000001": {{GameID: "published"}},
		}},
	}}
	service := NewReviewDeduplicator(repository, nil)
	service.discarder = discarder

	result, err := service.Deduplicate(t.Context(), model.ReviewDeduplicateRequest{ThroughItemID: through})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
	if result != (model.ReviewDeduplicateResult{}) {
		t.Fatalf("result = %+v", result)
	}
}
