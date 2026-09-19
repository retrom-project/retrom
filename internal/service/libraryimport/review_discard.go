package libraryimport

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/capability/security/authn"

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

