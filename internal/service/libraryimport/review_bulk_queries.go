package libraryimport

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

type ReviewBulkQueries struct{ repository ReviewBulkRepository }

func NewReviewBulkQueries(repository ReviewBulkRepository) *ReviewBulkQueries {
	return &ReviewBulkQueries{repository: repository}
}

func NormalizeReviewBulkScope(scope ReviewBulkScope) (ReviewBulkScope, error) {
	scope.Q = strings.ToLower(strings.Join(strings.Fields(scope.Q), " "))
	scope.TagID = strings.TrimSpace(scope.TagID)
	scope.ImportJobID = strings.TrimSpace(scope.ImportJobID)
	scope.SourceImportID = strings.TrimSpace(scope.SourceImportID)
	scope.PlatformInstanceID = strings.TrimSpace(scope.PlatformInstanceID)
	scope.BlockerCode = strings.TrimSpace(scope.BlockerCode)
	if !utf8.ValidString(scope.Q) || len([]rune(scope.Q)) > 200 || len(scope.BlockerCode) > 120 {
		return ReviewBulkScope{}, ErrReviewBulkQuery
	}
	sourceFilters := []string{scope.ImportJobID, scope.SourceImportID}
	count := 0
	for _, value := range sourceFilters {
		if value != "" {
			count++
		}
	}
	if count > 1 {
		return ReviewBulkScope{}, ErrReviewBulkQuery
	}
	for _, value := range []string{
		scope.TagID, scope.ImportJobID, scope.SourceImportID,
		scope.PlatformInstanceID,
	} {
		if value == "" {
			continue
		}
		parsed, err := uuid.Parse(value)
		if err != nil || parsed.String() != value {
			return ReviewBulkScope{}, ErrReviewBulkQuery
		}
	}
	return scope, nil
}

func normalizeReviewBulkCandidateQuery(query ReviewBulkCandidateQuery) (ReviewBulkCandidateQuery, error) {
	normalized, err := NormalizeReviewBulkScope(query.Scope)
	if err != nil {
		return ReviewBulkCandidateQuery{}, err
	}
	query.Scope = normalized
	if query.Limit == 0 {
		query.Limit = ReviewBulkQueryLimit
	}
	if query.Limit < 1 || query.Limit > ReviewBulkQueryLimit {
		return ReviewBulkCandidateQuery{}, ErrReviewBulkQuery
	}
	if err := validateReviewBulkCursor(query.AfterItemID); err != nil {
		return ReviewBulkCandidateQuery{}, err
	}
	if err := validateReviewBulkCursor(query.ThroughItemID); err != nil {
		return ReviewBulkCandidateQuery{}, err
	}
	if query.AfterItemID != "" && (query.ThroughItemID == "" || query.AfterItemID >= query.ThroughItemID) {
		return ReviewBulkCandidateQuery{}, ErrReviewBulkQuery
	}
	return query, nil
}

func validateReviewBulkCursor(value string) error {
	if value == "" {
		return nil
	}
	parsed, err := uuid.Parse(value)

	if err != nil || parsed.String() != value {
		return ErrReviewBulkQuery
	}
	return nil
}

func (service *ReviewBulkQueries) Candidates(
	ctx context.Context, scope ReviewBulkScope,
) ([]ReviewBulkCandidate, error) {
	return service.CandidatesPage(ctx, ReviewBulkCandidateQuery{Scope: scope})
}

func (service *ReviewBulkQueries) CandidatesPage(
	ctx context.Context, query ReviewBulkCandidateQuery,
) ([]ReviewBulkCandidate, error) {
	normalized, err := normalizeReviewBulkCandidateQuery(query)
	if err != nil {
		return nil, err
	}
	rows, err := service.repository.Candidates(ctx, normalized)
	if err != nil {
		return nil, fmt.Errorf("read review bulk candidates: %w", err)
	}
	return rows, nil
}
