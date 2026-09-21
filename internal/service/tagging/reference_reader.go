package tagging

import "context"

type ReferenceReader interface {
	References(context.Context, Owner) ([]Reference, error)
}

func ReviewDraftReferencesInScope(ctx context.Context, reader ReferenceReader, draftID string) ([]Reference, error) {
	refs, err := reader.References(ctx, Owner{Kind: OwnerReviewDraft, ID: draftID})
	return refs, repositoryError("review references", err)
}
