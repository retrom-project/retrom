package libraryimport

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/libraryimport"

	"github.com/google/uuid"
)

const reviewDeduplicatePageSize = 50

type reviewInScopeDiscarder interface {
	DiscardInScope(
		context.Context,
		model.ReviewDiscardScope,
		model.ReviewDiscardRequest,
	) (model.ReviewDecisionResult, error)
}

// ReviewDeduplicator applies the duplicate review policy while the repository
// keeps all SQL and transaction lifecycle details behind typed ports.
type ReviewDeduplicator struct {
	repository model.ReviewDeduplicateRepository
	discarder  reviewInScopeDiscarder
}

func NewReviewDeduplicator(repository model.ReviewDeduplicateRepository, now func() time.Time) *ReviewDeduplicator {
	return &ReviewDeduplicator{
		repository: repository,
		discarder:  NewReviewDiscards(nil, now),
	}
}

func (service *ReviewDeduplicator) Deduplicate(
	ctx context.Context, request model.ReviewDeduplicateRequest,
) (model.ReviewDeduplicateResult, error) {
	request, err := normalizeReviewDeduplicateRequest(request)
	if err != nil {
		return model.ReviewDeduplicateResult{}, err
	}
	var result model.ReviewDeduplicateResult
	err = service.repository.WithDeduplicate(ctx, func(scope model.ReviewDeduplicateScope) error {
		var deduplicateErr error
		result, deduplicateErr = service.deduplicateInScope(ctx, scope, request)
		return deduplicateErr
	})
	if err != nil {
		return model.ReviewDeduplicateResult{}, fmt.Errorf("deduplicate review items: %w", err)
	}
	return result, nil
}

func (service *ReviewDeduplicator) deduplicateInScope(
	ctx context.Context, scope model.ReviewDeduplicateScope, request model.ReviewDeduplicateRequest,
) (model.ReviewDeduplicateResult, error) {
	through, err := reviewDeduplicateThrough(ctx, scope.Reader, request.ThroughItemID)
	if err != nil || through == "" {
		return model.ReviewDeduplicateResult{}, err
	}
	candidates, err := scope.Reader.Candidates(ctx, model.ReviewBulkCandidateQuery{
		Scope:         request.Scope,
		AfterItemID:   request.AfterItemID,
		ThroughItemID: through,
		Limit:         reviewDeduplicatePageSize + 1,
	})
	if err != nil {
		return model.ReviewDeduplicateResult{}, fmt.Errorf("read review duplicate candidates: %w", err)
	}
	result := model.ReviewDeduplicateResult{ThroughItemID: &through}
	if len(candidates) > reviewDeduplicatePageSize {
		candidates = candidates[:reviewDeduplicatePageSize]
		result.NextAfterItemID = &candidates[len(candidates)-1].ItemID
	}
	duplicates := model.NewContentDuplicates(scope.Duplicates)
	for _, candidate := range candidates {
		result.ScannedCount++
		if candidate.AttachmentActive {
			result.AttachmentActiveCount++
			continue
		}
		discarded, err := service.discardDuplicate(ctx, scope, duplicates, candidate)
		if err != nil {
			return model.ReviewDeduplicateResult{}, err
		}
		if discarded {
			result.DiscardedCount++
		}
	}
	return result, nil
}

func reviewDeduplicateThrough(
	ctx context.Context, reader model.ReviewDeduplicateReader, through string,
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
	scope model.ReviewDeduplicateScope,
	duplicates *model.ContentDuplicates,
	candidate model.ReviewBulkCandidate,
) (bool, error) {
	games, err := duplicates.Matches(ctx, candidate.ItemID, candidate.PlatformID)
	if err != nil {
		return false, fmt.Errorf("read duplicate games: %w", err)
	}
	if len(games) == 0 {
		return false, nil
	}
	if _, err := service.discarder.DiscardInScope(ctx, scope.Discard, model.ReviewDiscardRequest{
		ItemID: candidate.ItemID, ExpectedVersion: candidate.ReviewVersion,
		Reason: "快速去重：游戏内容已发布", Mode: model.ReviewDiscardSingle,
	}); err != nil {
		return false, fmt.Errorf("discard duplicate review: %w", err)
	}
	return true, nil
}

func normalizeReviewDeduplicateRequest(request model.ReviewDeduplicateRequest) (model.ReviewDeduplicateRequest, error) {
	scope, err := NormalizeReviewBulkScope(request.Scope)
	if err != nil {
		return model.ReviewDeduplicateRequest{}, err
	}
	request.Scope = scope
	for _, value := range []string{request.AfterItemID, request.ThroughItemID} {
		if value == "" {
			continue
		}
		parsed, err := uuid.Parse(value)
		if err != nil || parsed.String() != value {
			return model.ReviewDeduplicateRequest{}, model.ErrReviewBulkQuery
		}
	}
	if request.AfterItemID != "" && (request.ThroughItemID == "" || request.AfterItemID >= request.ThroughItemID) {
		return model.ReviewDeduplicateRequest{}, model.ErrReviewBulkQuery
	}
	return request, nil
}
