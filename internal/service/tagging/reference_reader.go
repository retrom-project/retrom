package tagging

import (
	"context"

	model "retrom/internal/model/tagging"
)

func ReviewDraftReferencesInScope(ctx context.Context, reader model.ReferenceReader, draftID string) (
	[]model.Reference,
	error,
) {
	refs, err := reader.References(ctx, model.Owner{Kind: model.OwnerReviewDraft, ID: draftID})
	return refs, repositoryError("review references", err)
}
