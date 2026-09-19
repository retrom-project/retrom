package libraryimport

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/capability/security/authn"
	payloadreleasemodel "retrom/internal/model/payloadrelease"
	payloadreleaseservice "retrom/internal/service/payloadrelease"
	"retrom/internal/service/tagging"

	"github.com/google/uuid"
)

type ReviewDiscards struct {
	repository model.ReviewDiscardRepository
	now        func() time.Time
	newID      func() (string, error)
}

func NewReviewDiscards(repository model.ReviewDiscardRepository, now func() time.Time) *ReviewDiscards {
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
	ctx context.Context, request model.ReviewDiscardRequest,
) (model.ReviewDecisionResult, error) {
	request, err := normalizeReviewDiscard(request)
	if err != nil {
		return model.ReviewDecisionResult{}, err
	}
	eventID, err := service.newID()
	if err != nil {
		return model.ReviewDecisionResult{}, fmt.Errorf("create discard event ID: %w", err)
	}
	actor := discardActor(ctx)
	now := service.now().UnixMilli()
	return service.repository.CommitDiscard(ctx, model.DiscardCommand{
		Request: request,
		EventID: eventID,
		Actor:   actor,
		NowMS:   now,
	})
}

func discardActor(ctx context.Context) model.ReviewActor {
	actor := model.ReviewActor{Kind: "SYSTEM"}
	label := "release-setup"
	actor.Label = &label
	if principal, ok := authn.PrincipalFromContext(ctx); ok && principal.UserID != "" {
		actor.Kind = "USER"
		actor.UserID = &principal.UserID
		actor.Label = nil
	}
	return actor
}

// DiscardInScope shares the complete decision with a caller-owned transaction.
// Its result becomes durable only when that caller commits the whole operation.
func (service *ReviewDiscards) DiscardInScope(
	ctx context.Context, scope model.ReviewDiscardScope, request model.ReviewDiscardRequest,
) (model.ReviewDecisionResult, error) {
	request, err := normalizeReviewDiscard(request)
	if err != nil {
		return model.ReviewDecisionResult{}, err
	}
	snapshot, found, err := scope.Reader.Snapshot(ctx, request.ItemID)
	if err != nil {
		return model.ReviewDecisionResult{}, fmt.Errorf("read discard evidence: %w", err)
	}
	if !found || !canDiscardReview(snapshot, request) {
		return model.ReviewDecisionResult{}, model.ErrInvalid
	}
	tags, err := tagging.ReviewDraftReferencesInScope(ctx, scope.Tags, snapshot.DraftID)
	if err != nil {
		return model.ReviewDecisionResult{}, fmt.Errorf("read discard tags: %w", err)
	}
	event, err := reviewDiscardEvidence(ctx, snapshot, tags, request.Reason)
	if err != nil {
		return model.ReviewDecisionResult{}, err
	}
	event.ID, err = service.newID()
	if err != nil {
		return model.ReviewDecisionResult{}, fmt.Errorf("create discard event ID: %w", err)
	}
	event.ItemID = request.ItemID
	event.NowMS = service.now().UnixMilli()
	aggregate, err := projectReviewDiscardAggregate(snapshot.Aggregate, event.NowMS)
	if err != nil {
		return model.ReviewDecisionResult{}, err
	}
	change := model.ReviewDiscardChange{
		ItemID: request.ItemID, ImportID: snapshot.ImportID, ExpectedVersion: request.ExpectedVersion,
		NowMS: event.NowMS, Aggregate: aggregate,
	}
	if err := persistReviewDiscard(ctx, scope, request, change, event); err != nil {
		return model.ReviewDecisionResult{}, err
	}
	return model.ReviewDecisionResult{
		ItemID: request.ItemID, EventID: event.ID, Status: "DISCARDED",
		Version: snapshot.Version + 1, UpdatedAtMS: event.NowMS,
	}, nil
}

func normalizeReviewDiscard(request model.ReviewDiscardRequest) (model.ReviewDiscardRequest, error) {
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Mode == "" {
		request.Mode = model.ReviewDiscardSingle
	}
	if request.ItemID == "" || request.ExpectedVersion < 1 || !validField(request.Reason, 500, true) ||
		(request.Mode != model.ReviewDiscardSingle && request.Mode != model.ReviewDiscardBatch) {
		return model.ReviewDiscardRequest{}, model.ErrInvalid
	}
	return request, nil
}

func canDiscardReview(snapshot model.ReviewDiscardSnapshot, request model.ReviewDiscardRequest) bool {
	if snapshot.Version != request.ExpectedVersion || snapshot.Version == math.MaxInt64 ||
		snapshot.State != "REVIEW_PENDING" {
		return false
	}
	if request.Mode == model.ReviewDiscardBatch {
		return true
	}
	return !snapshot.SourceBusy && (snapshot.HandoffKind == "DIRECT" || snapshot.EmulationStationReady)
}

func persistReviewDiscard(
	ctx context.Context, scope model.ReviewDiscardScope, request model.ReviewDiscardRequest,
	change model.ReviewDiscardChange, event model.ReviewDiscardEvent,
) error {
	writer := scope.Writer
	if err := writer.CancelAttachments(ctx, request.ItemID, event.NowMS); err != nil {
		return fmt.Errorf("cancel discarded attachments: %w", err)
	}
	if err := writer.DiscardItem(ctx, change); err != nil {
		return fmt.Errorf("discard review and aggregate: %w", err)
	}
	if err := writer.RecordEvent(ctx, event); err != nil {
		return fmt.Errorf("record discarded review: %w", err)
	}
	if err := writer.TransitionOwner(ctx, model.ReviewOwnerTransition{
		ItemID: request.ItemID, State: model.ReviewOwnerDiscarded, Mode: request.Mode, NowMS: event.NowMS,
	}); err != nil {
		return fmt.Errorf("transition discarded review owner: %w", err)
	}
	if err := payloadreleaseservice.NewScheduler(nil).Review(
		ctx,
		scope.Payload,
		payloadreleasemodel.ReviewRelease{
			ItemID:   request.ItemID,
			ImportID: change.ImportID,
			Reason:   payloadreleasemodel.ReasonImportDiscarded,
			NowMS:    event.NowMS,
		},
	); err != nil {
		return fmt.Errorf("schedule discarded review payload: %w", err)
	}
	return nil
}
