package libraryimport

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const reviewDeduplicatePageSize = 50

// ReviewDeduplicateRepository owns the transaction used by a bounded
// deduplication pass. The application service supplies the policy and the
// repository supplies transaction-scoped readers and writers.
type ReviewDeduplicateRepository interface {
	WithDeduplicate(context.Context, func(ReviewDeduplicateScope) error) error
}

type ReviewDeduplicateScope struct {
	Reader     ReviewDeduplicateReader
	Duplicates ContentDuplicateReader
	Discard    ReviewDiscardScope
}

type ReviewDeduplicateReader interface {
	LatestReviewItemID(context.Context) (*string, error)
	Candidates(context.Context, ReviewBulkCandidateQuery) ([]ReviewBulkCandidate, error)
}

type ReviewDeduplicateRequest struct {
	Scope         ReviewBulkScope
	AfterItemID   string
	ThroughItemID string
}

type ReviewDeduplicateResult struct {
	ScannedCount          int
	DiscardedCount        int
	AttachmentActiveCount int
	NextAfterItemID       *string
	ThroughItemID         *string
}

type reviewInScopeDiscarder interface {
	DiscardInScope(context.Context, ReviewDiscardScope, ReviewDiscardRequest) (ReviewDecisionResult, error)
}

// ReviewDeduplicator applies the duplicate review policy while the repository
// keeps all SQL and transaction lifecycle details behind typed ports.
type ReviewDeduplicator struct {
	repository ReviewDeduplicateRepository
	discarder  reviewInScopeDiscarder
}

func NewReviewDeduplicator(repository ReviewDeduplicateRepository, now func() time.Time) *ReviewDeduplicator {
	return &ReviewDeduplicator{
		repository: repository,
		discarder:  NewReviewDiscards(nil, now),
	}
}

func (service *ReviewDeduplicator) Deduplicate(
	ctx context.Context, request ReviewDeduplicateRequest,
) (ReviewDeduplicateResult, error) {
	request, err := normalizeReviewDeduplicateRequest(request)
	if err != nil {
		return ReviewDeduplicateResult{}, err
	}
	var result ReviewDeduplicateResult
	err = service.repository.WithDeduplicate(ctx, func(scope ReviewDeduplicateScope) error {
		var deduplicateErr error
		result, deduplicateErr = service.deduplicateInScope(ctx, scope, request)
		return deduplicateErr
	})
	if err != nil {
		return ReviewDeduplicateResult{}, fmt.Errorf("deduplicate review items: %w", err)
	}
	return result, nil
}

func (service *ReviewDeduplicator) deduplicateInScope(
	ctx context.Context, scope ReviewDeduplicateScope, request ReviewDeduplicateRequest,
) (ReviewDeduplicateResult, error) {
	through, err := reviewDeduplicateThrough(ctx, scope.Reader, request.ThroughItemID)
	if err != nil || through == "" {
		return ReviewDeduplicateResult{}, err
	}
	candidates, err := scope.Reader.Candidates(ctx, ReviewBulkCandidateQuery{
		Scope:         request.Scope,
		AfterItemID:   request.AfterItemID,
		ThroughItemID: through,
		Limit:         reviewDeduplicatePageSize + 1,
	})
	if err != nil {
		return ReviewDeduplicateResult{}, fmt.Errorf("read review duplicate candidates: %w", err)
	}
	result := ReviewDeduplicateResult{ThroughItemID: &through}
	if len(candidates) > reviewDeduplicatePageSize {
		candidates = candidates[:reviewDeduplicatePageSize]
		result.NextAfterItemID = &candidates[len(candidates)-1].ItemID
	}
	duplicates := NewContentDuplicates(scope.Duplicates)
	for _, candidate := range candidates {
		result.ScannedCount++
		if candidate.AttachmentActive {
			result.AttachmentActiveCount++
			continue
		}
		discarded, err := service.discardDuplicate(ctx, scope, duplicates, candidate)
		if err != nil {
			return ReviewDeduplicateResult{}, err
		}
		if discarded {
			result.DiscardedCount++
		}
	}
	return result, nil
}

func reviewDeduplicateThrough(
	ctx context.Context, reader ReviewDeduplicateReader, through string,
) (string, error) {
	if through != "" {
		return through, nil
	}
	value, err := reader.LatestReviewItemID(ctx)
	if err != nil {
		return "", fmt.Errorf("read deduplication upper bound: %w", err)
	}
	if value == nil {
		return "", nil
	}
	return *value, nil
}

func (service *ReviewDeduplicator) discardDuplicate(
	ctx context.Context,
	scope ReviewDeduplicateScope,
	duplicates *ContentDuplicates,
	candidate ReviewBulkCandidate,
) (bool, error) {
	games, err := duplicates.Matches(ctx, candidate.ItemID, candidate.PlatformID)
	if err != nil {
		return false, fmt.Errorf("read duplicate games: %w", err)
	}
	if len(games) == 0 {
		return false, nil
	}
	if _, err := service.discarder.DiscardInScope(ctx, scope.Discard, ReviewDiscardRequest{
		ItemID: candidate.ItemID, ExpectedVersion: candidate.ReviewVersion,
		Reason: "快速去重：游戏内容已发布", Mode: ReviewDiscardSingle,
	}); err != nil {
		return false, fmt.Errorf("discard duplicate review: %w", err)
	}
	return true, nil
}

func normalizeReviewDeduplicateRequest(request ReviewDeduplicateRequest) (ReviewDeduplicateRequest, error) {
	scope, err := NormalizeReviewBulkScope(request.Scope)
	if err != nil {
		return ReviewDeduplicateRequest{}, err
	}
	request.Scope = scope
	for _, value := range []string{request.AfterItemID, request.ThroughItemID} {
		if value == "" {
			continue
		}
		parsed, err := uuid.Parse(value)
		if err != nil || parsed.String() != value {
			return ReviewDeduplicateRequest{}, ErrReviewBulkQuery
		}
	}
	if request.AfterItemID != "" && (request.ThroughItemID == "" || request.AfterItemID >= request.ThroughItemID) {
		return ReviewDeduplicateRequest{}, ErrReviewBulkQuery
	}
	return request, nil
}
