package tagging

import "context"

func ReviewDraftReferencesInScope(ctx context.Context, reader ReferenceReader, draftID string) ([]Reference, error) {
	refs, err := reader.References(ctx, Owner{Kind: OwnerReviewDraft, ID: draftID})
	return refs, repositoryError("review references", err)
}
