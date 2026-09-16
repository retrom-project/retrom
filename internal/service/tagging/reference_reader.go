package tagging

import (
	"context"

	model "retrom/internal/model/tagging"
)

// ReviewDraftReferencesInScope reads draft tag references using a relation reader.
func ReviewDraftReferencesInScope(
	ctx context.Context, reader model.ReferenceReader, draftID string,
) ([]model.Reference, error) {
	refs, err := reader.References(ctx, model.Owner{Kind: model.OwnerReviewDraft, ID: draftID})
	return refs, repositoryError("review references", err)
}
