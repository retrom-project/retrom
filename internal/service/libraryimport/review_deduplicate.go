package libraryimport

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/capability/security/authn"

	"github.com/google/uuid"
)

const reviewDeduplicatePageSize = 50

// ReviewDeduplicator applies the duplicate review policy while the repository
// keeps all SQL and transaction lifecycle details behind typed ports.
type ReviewDeduplicator struct {
	repository model.ReviewDeduplicateRepository
	newID      func() (string, error)
	now        func() time.Time
}

func NewReviewDeduplicator(repository model.ReviewDeduplicateRepository, now func() time.Time) *ReviewDeduplicator {
	return &ReviewDeduplicator{
		repository: repository,
		newID: func() (string, error) {
			id, err := uuid.NewV7()
			if err != nil {
				return "", err
			}
			return id.String(), nil
		},
		now: now,
	}
}

func (service *ReviewDeduplicator) Deduplicate(
	ctx context.Context, request model.ReviewDeduplicateRequest,
) (model.ReviewDeduplicateResult, error) {
	request, err := normalizeReviewDeduplicateRequest(request)
	if err != nil {
		return model.ReviewDeduplicateResult{}, err
	}
	slots := make([]model.DeduplicateDiscardSlot, reviewDeduplicatePageSize)
	for i := range slots {
		id, err := service.newID()
		if err != nil {
			return model.ReviewDeduplicateResult{}, fmt.Errorf("create discard event ID: %w", err)
		}
		slots[i] = model.DeduplicateDiscardSlot{EventID: id}
	}
	actor := deduplicateActor(ctx)
	now := service.now().UnixMilli()
	return service.repository.CommitDeduplicate(ctx, model.DeduplicateCommand{
		Request:  request,
		Discards: slots,
		NowMS:    now,
		Actor:    actor,
	})
}

func deduplicateActor(ctx context.Context) model.ReviewActor {
	actor := model.ReviewActor{Kind: "SYSTEM"}
	label := "deduplication"
	actor.Label = &label
	if principal, ok := authn.PrincipalFromContext(ctx); ok && principal.UserID != "" {
		actor.Kind = "USER"
		actor.UserID = &principal.UserID
		actor.Label = nil
	}
	return actor
}

func normalizeReviewDeduplicateRequest(request model.ReviewDeduplicateRequest) (model.ReviewDeduplicateRequest, error) {
	scope, err := NormalizeReviewBulkScope(request.Scope)
	if err != nil {
		return model.ReviewDeduplicateRequest{}, err
	}
	request.Scope = scope
	for _, value := range []string{request.AfterItemID, request.ThroughItemID} {
		if value == "" {
			continue
		}
		parsed, err := uuid.Parse(value)
		if err != nil || parsed.String() != value {
			return model.ReviewDeduplicateRequest{}, model.ErrReviewBulkQuery
		}
	}
	if request.AfterItemID != "" && (request.ThroughItemID == "" || request.AfterItemID >= request.ThroughItemID) {
		return model.ReviewDeduplicateRequest{}, model.ErrReviewBulkQuery
	}
	return request, nil
}
