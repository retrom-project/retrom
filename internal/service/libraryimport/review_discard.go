package libraryimport

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"retrom/internal/service/idempotency"

	importcleanup "retrom/internal/service/libraryimport/payloadpolicy"

	"retrom/internal/service/cleanupjobs"
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
		if discardErr != nil {
			return discardErr
		}
		return idempotency.Complete(ctx, idempotency.Result{Value: result})
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
		NowMS: now, Aggregate: aggregate, Existing: request.Existing,
	}
	if err := persistReviewDiscard(ctx, scope, request, change); err != nil {
		return ReviewDecisionResult{}, err
	}
	status := "DISCARDED"
	if request.Existing != nil {
		status = "SKIPPED_EXISTING"
	}
	return ReviewDecisionResult{
		ItemID: request.ItemID, Status: status,
		Version: snapshot.Version + 1, UpdatedAtMS: now,
	}, nil
}

func normalizeReviewDiscard(request ReviewDiscardRequest) (ReviewDiscardRequest, error) {
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Mode == "" {
		request.Mode = ReviewDiscardSingle
	}
	if request.Existing != nil && (len(request.Existing.Games) == 0 || len(request.Existing.Identity) != 64) {
		return ReviewDiscardRequest{}, ErrInvalid
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
	owner := ReviewOwnerTransition{ItemID: request.ItemID, State: ReviewOwnerDiscarded, Mode: request.Mode, NowMS: now}
	if request.Existing != nil {
		owner.State, owner.GameID = ReviewOwnerExisting, &request.Existing.Games[0].GameID
	}
	if err := writer.TransitionOwner(ctx, owner); err != nil {
		return fmt.Errorf("transition discarded review owner: %w", err)
	}
	if err := importcleanup.Review(ctx, cleanupjobs.NewScheduler(nil),
		scope.Payload,
		importcleanup.ReviewRelease{
			ItemID:   request.ItemID,
			ImportID: change.ImportID,
			Reason:   cleanupjobs.ReasonImportDiscarded,
			NowMS:    now,
		},
	); err != nil {
		return fmt.Errorf("schedule discarded review payload: %w", err)
	}
	return nil
}
