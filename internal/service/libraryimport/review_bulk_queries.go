package libraryimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	model "retrom/internal/model/libraryimport"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

type ReviewBulkQueries struct{ repository model.ReviewBulkRepository }

func NewReviewBulkQueries(repository model.ReviewBulkRepository) *ReviewBulkQueries {
	return &ReviewBulkQueries{repository: repository}
}

func NormalizeReviewBulkScope(scope model.ReviewBulkScope) (model.ReviewBulkScope, error) {
	scope.Q = strings.ToLower(strings.Join(strings.Fields(scope.Q), " "))
	scope.TagID = strings.TrimSpace(scope.TagID)
	scope.ImportJobID = strings.TrimSpace(scope.ImportJobID)
	scope.PegasusImportID = strings.TrimSpace(scope.PegasusImportID)
	scope.EmulationStationImportID = strings.TrimSpace(scope.EmulationStationImportID)
	scope.PlatformInstanceID = strings.TrimSpace(scope.PlatformInstanceID)
	scope.BlockerCode = strings.TrimSpace(scope.BlockerCode)
	if !utf8.ValidString(scope.Q) || len([]rune(scope.Q)) > 200 || len(scope.BlockerCode) > 120 {
		return model.ReviewBulkScope{}, model.ErrReviewBulkQuery
	}
	sourceFilters := []string{scope.ImportJobID, scope.PegasusImportID, scope.EmulationStationImportID}
	count := 0
	for _, value := range sourceFilters {
		if value != "" {
			count++
		}
	}
	if count > 1 {
		return model.ReviewBulkScope{}, model.ErrReviewBulkQuery
	}
	for _, value := range []string{
		scope.TagID, scope.ImportJobID, scope.PegasusImportID,
		scope.EmulationStationImportID, scope.PlatformInstanceID,
	} {
		if value == "" {
			continue
		}
		parsed, err := uuid.Parse(value)
		if err != nil || parsed.String() != value {
			return model.ReviewBulkScope{}, model.ErrReviewBulkQuery
		}
	}
	return scope, nil
}

func normalizeReviewBulkCandidateQuery(query model.ReviewBulkCandidateQuery) (model.ReviewBulkCandidateQuery, error) {
	normalized, err := NormalizeReviewBulkScope(query.Scope)
	if err != nil {
		return model.ReviewBulkCandidateQuery{}, err
	}
	query.Scope = normalized
	if query.Limit == 0 {
		query.Limit = model.ReviewBulkQueryLimit
	}
	if query.Limit < 1 || query.Limit > model.ReviewBulkQueryLimit {
		return model.ReviewBulkCandidateQuery{}, model.ErrReviewBulkQuery
	}
	if err := validateReviewBulkCursor(query.AfterItemID); err != nil {
		return model.ReviewBulkCandidateQuery{}, err
	}
	if err := validateReviewBulkCursor(query.ThroughItemID); err != nil {
		return model.ReviewBulkCandidateQuery{}, err
	}
	if query.AfterItemID != "" && (query.ThroughItemID == "" || query.AfterItemID >= query.ThroughItemID) {
		return model.ReviewBulkCandidateQuery{}, model.ErrReviewBulkQuery
	}
	return query, nil
}

func validateReviewBulkCursor(value string) error {
	if value == "" {
		return nil
	}
	parsed, err := uuid.Parse(value)

	if err != nil || parsed.String() != value {
		return model.ErrReviewBulkQuery
	}
	return nil
}

func (service *ReviewBulkQueries) Candidates(
	ctx context.Context, scope model.ReviewBulkScope,
) ([]model.ReviewBulkCandidate, error) {
	return service.CandidatesPage(ctx, model.ReviewBulkCandidateQuery{Scope: scope})
}

func (service *ReviewBulkQueries) CandidatesPage(
	ctx context.Context, query model.ReviewBulkCandidateQuery,
) ([]model.ReviewBulkCandidate, error) {
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
) (model.ReviewBulkItemPage, error) {
	if err := validateReviewBulkCursor(bulkID); err != nil {
		return model.ReviewBulkItemPage{}, err
	}
	if outcome != "" && !validReviewBulkItemOutcome(outcome) {
		return model.ReviewBulkItemPage{}, model.ErrReviewBulkQuery
	}
	if limit == 0 {
		limit = model.ReviewBulkItemLimit
	}
	if limit < 1 || limit > model.ReviewBulkItemLimit {
		return model.ReviewBulkItemPage{}, model.ErrReviewBulkQuery
	}
	after := -1
	if cursor != "" {
		parsed, err := strconv.Atoi(cursor)
		if err != nil || parsed < 0 {
			return model.ReviewBulkItemPage{}, model.ErrReviewBulkQuery
		}
		after = parsed
	}
	rows, err := service.repository.Items(ctx, model.ReviewBulkItemQuery{
		BulkApprovalID: bulkID, Outcome: outcome, AfterOrdinal: after, Limit: limit + 1,
	})
	if err != nil {
		return model.ReviewBulkItemPage{}, fmt.Errorf("read review bulk items: %w", err)
	}
	page := model.ReviewBulkItemPage{Items: make([]model.ReviewBulkItemRecord, 0, minInt(limit, len(rows)))}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1].Ordinal
		next := strconv.Itoa(last)
		page.NextCursor = &next
	}
	page.Items = append(page.Items, rows...)
	return page, nil
}

func (service *ReviewBulkQueries) Summary(ctx context.Context, bulkID string) (model.ReviewBulkSummary, error) {
	if err := validateReviewBulkCursor(bulkID); err != nil {
		return model.ReviewBulkSummary{}, err
	}
	result, err := service.repository.Summary(ctx, bulkID)
	if err != nil {
		return model.ReviewBulkSummary{}, fmt.Errorf("read review bulk summary: %w", err)
	}
	return result, nil
}

func (service *ReviewBulkQueries) ActiveSummary(ctx context.Context) (model.ReviewBulkSummary, bool, error) {
	result, found, err := service.repository.ActiveSummary(ctx)
	if err != nil {
		return model.ReviewBulkSummary{}, false, fmt.Errorf("read active review bulk summary: %w", err)
	}
	return result, found, nil
}

func ReviewBulkScopeDigest(scope model.ReviewBulkScope) (string, string, error) {
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

func ReviewBulkCandidateManifestDigest(candidates []model.ReviewBulkCandidate) string {
	ordered := append([]model.ReviewBulkCandidate(nil), candidates...)
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
