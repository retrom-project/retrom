package libraryimport

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/service/tagging"
)

type ReviewDiscards struct {
	repository ReviewDiscardRepository
	now        func() time.Time
	newID      func() (string, error)
}

func NewReviewDiscards(repository ReviewDiscardRepository, now func() time.Time) *ReviewDiscards {
	return &ReviewDiscards{repository: repository, now: now, newID: newReviewDiscardID}
}

func newReviewDiscardID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("create review discard ID: %w", err)
	}
	return id.String(), nil
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
	tags, err := tagging.ReviewDraftReferencesInScope(ctx, scope.Tags, snapshot.DraftID)
	if err != nil {
		return ReviewDecisionResult{}, fmt.Errorf("read discard tags: %w", err)
	}
	event, err := reviewDiscardEvidence(ctx, snapshot, tags, request.Reason)
	if err != nil {
		return ReviewDecisionResult{}, err
	}
	event.ID, err = service.newID()
	if err != nil {
		return ReviewDecisionResult{}, fmt.Errorf("create discard event ID: %w", err)
	}
	event.ItemID = request.ItemID
	event.NowMS = service.now().UnixMilli()
	aggregate, err := projectReviewDiscardAggregate(snapshot.Aggregate, event.NowMS)
	if err != nil {
		return ReviewDecisionResult{}, err
	}
	change := ReviewDiscardChange{
		ItemID: request.ItemID, ImportID: snapshot.ImportID, ExpectedVersion: request.ExpectedVersion,
		NowMS: event.NowMS, Aggregate: aggregate,
	}
	if err := persistReviewDiscard(ctx, scope.Writer, request, change, event); err != nil {
		return ReviewDecisionResult{}, err
	}
	return ReviewDecisionResult{
		ItemID: request.ItemID, EventID: event.ID, Status: "DISCARDED",
		Version: snapshot.Version + 1, UpdatedAtMS: event.NowMS,
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
	return !snapshot.SourceBusy && (snapshot.HandoffKind == "DIRECT" || snapshot.EmulationStationReady)
}

func persistReviewDiscard(
	ctx context.Context, writer ReviewDiscardWriter, request ReviewDiscardRequest,
	change ReviewDiscardChange, event ReviewDiscardEvent,
) error {
	if err := writer.CancelAttachments(ctx, request.ItemID, event.NowMS); err != nil {
		return fmt.Errorf("cancel discarded attachments: %w", err)
	}
	if err := writer.DiscardItem(ctx, change); err != nil {
		return fmt.Errorf("discard review and aggregate: %w", err)
	}
	if err := writer.RecordEvent(ctx, event); err != nil {
		return fmt.Errorf("record discarded review: %w", err)
	}
	if err := writer.TransitionOwner(ctx, ReviewOwnerTransition{
		ItemID: request.ItemID, State: ReviewOwnerDiscarded, Mode: request.Mode, NowMS: event.NowMS,
	}); err != nil {
		return fmt.Errorf("transition discarded review owner: %w", err)
	}
	if err := writer.SchedulePayload(ctx, ReviewPayloadRelease{
		ItemID: request.ItemID, ImportID: change.ImportID, Outcome: ReviewOwnerDiscarded, NowMS: event.NowMS,
	}); err != nil {
		return fmt.Errorf("schedule discarded review payload: %w", err)
	}
	return nil
}
