package libraryimport

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"retrom/internal/service/payloadrelease"
)

type ReviewDiscards struct {
	repository ReviewDiscardRepository
	now        func() time.Time
}

func NewReviewDiscards(repository ReviewDiscardRepository, now func() time.Time) *ReviewDiscards {
	return &ReviewDiscards{repository: repository, now: now}
}

func (service *ReviewDiscards) Discard(
	ctx context.Context, request ReviewDiscardRequest,
) (ReviewDecisionResult, error) {
	request, err := normalizeReviewDiscard(request)
	if err != nil {
		return ReviewDecisionResult{}, err
	}
	var result ReviewDecisionResult
	err = service.repository.WithDiscard(ctx, func(scope ReviewDiscardScope) error {
		var discardErr error
		result, discardErr = service.DiscardInScope(ctx, scope, request)
		return discardErr
	})
	if err != nil {
		return ReviewDecisionResult{}, fmt.Errorf("commit review discard: %w", err)
	}
	return result, nil
}

// DiscardInScope shares the complete decision with a caller-owned transaction.
// Its result becomes durable only when that caller commits the whole operation.
func (service *ReviewDiscards) DiscardInScope(
	ctx context.Context, scope ReviewDiscardScope, request ReviewDiscardRequest,
) (ReviewDecisionResult, error) {
	request, err := normalizeReviewDiscard(request)
	if err != nil {
		return ReviewDecisionResult{}, err
	}
	snapshot, found, err := scope.Reader.Snapshot(ctx, request.ItemID)
	if err != nil {
		return ReviewDecisionResult{}, fmt.Errorf("read discard evidence: %w", err)
	}
	if !found || !canDiscardReview(snapshot, request) {
		return ReviewDecisionResult{}, ErrInvalid
	}
	now := service.now().UnixMilli()
	aggregate, err := projectReviewDiscardAggregate(snapshot.Aggregate, now)
	if err != nil {
		return ReviewDecisionResult{}, err
	}
	change := ReviewDiscardChange{
		ItemID: request.ItemID, ImportID: snapshot.ImportID, ExpectedVersion: request.ExpectedVersion,
		NowMS: now, Aggregate: aggregate,
	}
	if err := persistReviewDiscard(ctx, scope, request, change); err != nil {
		return ReviewDecisionResult{}, err
	}
	return ReviewDecisionResult{
		ItemID: request.ItemID, Status: "DISCARDED",
		Version: snapshot.Version + 1, UpdatedAtMS: now,
	}, nil
}

func normalizeReviewDiscard(request ReviewDiscardRequest) (ReviewDiscardRequest, error) {
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Mode == "" {
		request.Mode = ReviewDiscardSingle
	}
	if request.ItemID == "" || request.ExpectedVersion < 1 || !validField(request.Reason, 500, true) ||
		(request.Mode != ReviewDiscardSingle && request.Mode != ReviewDiscardBatch) {
		return ReviewDiscardRequest{}, ErrInvalid
	}
	return request, nil
}

func canDiscardReview(snapshot ReviewDiscardSnapshot, request ReviewDiscardRequest) bool {
	if snapshot.Version != request.ExpectedVersion || snapshot.Version == math.MaxInt64 ||
		snapshot.State != "REVIEW_PENDING" {
		return false
	}
	if request.Mode == ReviewDiscardBatch {
		return true
	}
	return !snapshot.SourceBusy
}

func persistReviewDiscard(
	ctx context.Context, scope ReviewDiscardScope, request ReviewDiscardRequest,
	change ReviewDiscardChange,
) error {
	writer := scope.Writer
	now := change.NowMS
	if err := writer.CancelAttachments(ctx, request.ItemID, now); err != nil {
		return fmt.Errorf("cancel discarded attachments: %w", err)
	}
	if err := writer.DiscardItem(ctx, change); err != nil {
		return fmt.Errorf("discard review and aggregate: %w", err)
	}
	if err := writer.TransitionOwner(ctx, ReviewOwnerTransition{
		ItemID: request.ItemID, State: ReviewOwnerDiscarded, Mode: request.Mode, NowMS: now,
	}); err != nil {
		return fmt.Errorf("transition discarded review owner: %w", err)
	}
	if err := payloadrelease.NewScheduler(nil).Review(
		ctx,
		scope.Payload,
		payloadrelease.ReviewRelease{
			ItemID:   request.ItemID,
			ImportID: change.ImportID,
			Reason:   payloadrelease.ReasonImportDiscarded,
			NowMS:    now,
		},
	); err != nil {
		return fmt.Errorf("schedule discarded review payload: %w", err)
	}
	return nil
}
