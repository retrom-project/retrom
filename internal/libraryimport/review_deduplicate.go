package libraryimport

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"retrom/internal/cleanup"

	"github.com/google/uuid"
)

const reviewDeduplicatePageSize = 50

type ReviewDeduplicateRequest struct {
	Scope         ReviewBulkScope `json:"scope"`
	AfterItemID   string          `json:"afterItemId,omitempty"`
	ThroughItemID string          `json:"throughItemId,omitempty"`
}

type ReviewDeduplicateResult struct {
	ScannedCount          int     `json:"scannedCount"`
	DiscardedCount        int     `json:"discardedCount"`
	AttachmentActiveCount int     `json:"attachmentActiveCount"`
	NextAfterItemID       *string `json:"nextAfterItemId"`
	ThroughItemID         *string `json:"throughItemId"`
}

func normalizeReviewDeduplicateRequest(request ReviewDeduplicateRequest) (ReviewDeduplicateRequest, error) {
	scope, err := normalizeReviewBulkScope(request.Scope)
	if err != nil {
		return request, err
	}
	request.Scope = scope
	for _, value := range []string{request.AfterItemID, request.ThroughItemID} {
		if value != "" {
			parsed, err := uuid.Parse(value)
			if err != nil || parsed.String() != value {
				return request, ErrReviewBulkInvalidScope
			}
		}
	}
	if request.AfterItemID != "" && (request.ThroughItemID == "" || request.AfterItemID >= request.ThroughItemID) {
		return request, ErrReviewBulkInvalidScope
	}
	return request, nil
}

// DeduplicateReviews checks and discards one bounded page in a single transaction.
// The first page freezes an upper ID bound so new imports cannot extend this run.
func (service *Service) DeduplicateReviews(
	ctx context.Context, request ReviewDeduplicateRequest,
) (ReviewDeduplicateResult, error) {
	result := ReviewDeduplicateResult{}
	request, err := normalizeReviewDeduplicateRequest(request)
	if err != nil {
		return result, err
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("libraryimport/deduplicate: %w", err)
	}
	defer cleanup.Rollback(transaction)
	through := sql.NullString{String: request.ThroughItemID, Valid: request.ThroughItemID != ""}
	if !through.Valid {
		if err := transaction.QueryRowContext(ctx,
			`SELECT max(id) FROM import_items WHERE state='REVIEW_PENDING'`,
		).Scan(&through); err != nil {
			return result, fmt.Errorf("libraryimport/deduplicate upper bound: %w", err)
		}
	}
	if !through.Valid {
		return result, nil
	}
	result.ThroughItemID = &through.String
	query, arguments := reviewBulkCandidatesQuery(request.Scope)
	query = strings.TrimSuffix(query, " ORDER BY item.id") + " AND item.id>? AND item.id<=? ORDER BY item.id LIMIT ?"
	arguments = append(arguments, request.AfterItemID, through.String, reviewDeduplicatePageSize+1)
	candidates, err := scanReviewBulkCandidateQuery(ctx, transaction, query, arguments)
	if err != nil {
		return ReviewDeduplicateResult{}, err
	}
	if len(candidates) > reviewDeduplicatePageSize {
		candidates = candidates[:reviewDeduplicatePageSize]
		result.NextAfterItemID = &candidates[len(candidates)-1].itemID
	}
	for _, candidate := range candidates {
		result.ScannedCount++
		if candidate.attachmentActive {
			result.AttachmentActiveCount++
			continue
		}
		duplicates, err := findDuplicateGames(ctx, transaction, candidate.itemID, candidate.platformID)
		if err != nil {
			return ReviewDeduplicateResult{}, err
		}
		if len(duplicates) == 0 {
			continue
		}
		if _, err := service.discardInTransaction(
			ctx, transaction, candidate.itemID, candidate.reviewVersion, "快速去重：游戏内容已发布", false,
		); err != nil {
			return ReviewDeduplicateResult{}, err
		}
		result.DiscardedCount++
	}
	if err := transaction.Commit(); err != nil {
		return ReviewDeduplicateResult{}, fmt.Errorf("libraryimport/deduplicate commit: %w", err)
	}
	return result, nil
}
