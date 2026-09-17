package libraryimport

import (
	"context"
	"errors"
	"fmt"

	librarycomposition "retrom/internal/bootstrap/composition/libraryimport"
	libraryservice "retrom/internal/model/libraryimport"

	"github.com/google/uuid"
)

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
	value, err := librarycomposition.NewReviewDeduplicator(service.database, service.now).Deduplicate(
		ctx, libraryservice.ReviewDeduplicateRequest{
			Scope: libraryservice.ReviewBulkScope{
				Q: request.Scope.Q, TagID: request.Scope.TagID, ImportJobID: request.Scope.ImportJobID,
				PegasusImportID:          request.Scope.PegasusImportID,
				EmulationStationImportID: request.Scope.EmulationStationImportID,
				PlatformInstanceID:       request.Scope.PlatformInstanceID,
				BlockerCode:              request.Scope.BlockerCode,
			},
			AfterItemID: request.AfterItemID, ThroughItemID: request.ThroughItemID,
		},
	)
	if err != nil {
		if errors.Is(err, libraryservice.ErrReviewBulkQuery) {
			return ReviewDeduplicateResult{}, ErrReviewBulkInvalidScope
		}
		return ReviewDeduplicateResult{}, fmt.Errorf("libraryimport/deduplicate: %w", err)
	}
	return ReviewDeduplicateResult{
		ScannedCount: value.ScannedCount, DiscardedCount: value.DiscardedCount,
		AttachmentActiveCount: value.AttachmentActiveCount, NextAfterItemID: value.NextAfterItemID,
		ThroughItemID: value.ThroughItemID,
	}, nil
}
