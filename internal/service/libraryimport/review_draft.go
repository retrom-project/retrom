package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/capability/security/authn"
)

// ReviewDrafts coordinates review draft commands while the repository owns
// transaction boundaries and SQL. Keeping this small command service makes
// the same operation usable by HTTP handlers and non-HTTP import workflows.
type ReviewDrafts struct {
	repository ReviewDraftPatchRepository
}

func NewReviewDrafts(repository ReviewDraftPatchRepository) *ReviewDrafts {
	return &ReviewDrafts{repository: repository}
}

func (service *ReviewDrafts) Patch(
	ctx context.Context, itemID string, expectedVersion int64, patch DraftPatch,
) (DraftResult, error) {
	if err := ValidateDraftPatch(patch); err != nil {
		return DraftResult{}, err
	}
	result, err := service.repository.Patch(ctx, ReviewDraftPatchRequest{
		ItemID: itemID, ExpectedVersion: expectedVersion, Patch: patch,
		Actor: authn.ActorFromContext(ctx, "release-setup"),
	})
	if err != nil {
		return DraftResult{}, fmt.Errorf("patch review draft: %w", err)
	}
	return result, nil
}
