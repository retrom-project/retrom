package libraryimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
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

func (service *ReviewBulkQueries) Items(
	ctx context.Context, bulkID, outcome, cursor string, limit int,
) (ReviewBulkItemPage, error) {
	if err := validateReviewBulkCursor(bulkID); err != nil {
		return ReviewBulkItemPage{}, err
	}
	if outcome != "" && !validReviewBulkItemOutcome(outcome) {
		return ReviewBulkItemPage{}, ErrReviewBulkQuery
	}
	if limit == 0 {
		limit = ReviewBulkItemLimit
	}
	if limit < 1 || limit > ReviewBulkItemLimit {
		return ReviewBulkItemPage{}, ErrReviewBulkQuery
	}
	after := -1
	if cursor != "" {
		parsed, err := strconv.Atoi(cursor)
		if err != nil || parsed < 0 {
			return ReviewBulkItemPage{}, ErrReviewBulkQuery
		}
		after = parsed
	}
	rows, err := service.repository.Items(ctx, ReviewBulkItemQuery{
		BulkApprovalID: bulkID, Outcome: outcome, AfterOrdinal: after, Limit: limit + 1,
	})
	if err != nil {
		return ReviewBulkItemPage{}, fmt.Errorf("read review bulk items: %w", err)
	}
	page := ReviewBulkItemPage{Items: make([]ReviewBulkItemRecord, 0, minInt(limit, len(rows)))}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1].Ordinal
		next := strconv.Itoa(last)
		page.NextCursor = &next
	}
	page.Items = append(page.Items, rows...)
	return page, nil
}

func (service *ReviewBulkQueries) Summary(ctx context.Context, bulkID string) (ReviewBulkSummary, error) {
	if err := validateReviewBulkCursor(bulkID); err != nil {
		return ReviewBulkSummary{}, err
	}
	result, err := service.repository.Summary(ctx, bulkID)
	if err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("read review bulk summary: %w", err)
	}
	return result, nil
}

func (service *ReviewBulkQueries) ActiveSummary(ctx context.Context) (ReviewBulkSummary, bool, error) {
	result, found, err := service.repository.ActiveSummary(ctx)
	if err != nil {
		return ReviewBulkSummary{}, false, fmt.Errorf("read active review bulk summary: %w", err)
	}
	return result, found, nil
}

func ReviewBulkScopeDigest(scope ReviewBulkScope) (string, string, error) {
	normalized, err := NormalizeReviewBulkScope(scope)
	if err != nil {
		return "", "", err
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", "", fmt.Errorf("encode review bulk scope: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return string(encoded), hex.EncodeToString(digest[:]), nil
}

func ReviewBulkCandidateManifestDigest(candidates []ReviewBulkCandidate) string {
	ordered := append([]ReviewBulkCandidate(nil), candidates...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].ItemID < ordered[right].ItemID })
	digest := sha256.New()
	for _, candidate := range ordered {
		_, _ = fmt.Fprintf(digest, "%s\x00%d\x00%s\x00%s\n", candidate.ItemID,
			candidate.ReviewVersion, optionalReviewBulkString(candidate.ValidationID), candidate.SourceSnapshotID)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func optionalReviewBulkString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func validReviewBulkItemOutcome(value string) bool {
	switch value {
	case "PENDING", "RUNNING", "PUBLISHED", "SKIPPED_DUPLICATE",
		"SKIPPED_CHANGED", "SKIPPED_NOT_READY", "FAILED_FINAL", "CANCELLED":
		return true
	default:
		return false
	}
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
