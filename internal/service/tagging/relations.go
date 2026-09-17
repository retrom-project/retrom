package tagging

import (
	"context"
	"fmt"

	model "retrom/internal/model/tagging"
)

type activeReferenceReader interface {
	ActiveReferences(context.Context, []string) ([]model.Reference, error)
}

// ValidateActiveReferences validates tag IDs against active database state.
func ValidateActiveReferences(
	ctx context.Context, reader activeReferenceReader, tagIDs []string,
) ([]model.Reference, error) {
	validated, err := model.ValidateIDs(tagIDs)
	if err != nil {
		return nil, fmt.Errorf("validate IDs: %w", err)
	}
	if len(validated) == 0 {
		return []model.Reference{}, nil
	}
	result, err := reader.ActiveReferences(ctx, validated)
	if err != nil {
		return nil, repositoryError("read active references", err)
	}
	refs, factsErr := model.ValidateActiveReferenceFacts(validated, result)
	if factsErr != nil {
		return nil, fmt.Errorf("validate active references: %w", factsErr)
	}
	return refs, nil
}

// ReviewDraftReferencesInScope reads draft tag references using a relation reader.
func ReviewDraftReferencesInScope(
	ctx context.Context, reader model.ReferenceReader, draftID string,
) ([]model.Reference, error) {
	refs, err := reader.References(ctx, model.Owner{Kind: model.OwnerReviewDraft, ID: draftID})
	return refs, repositoryError("read review draft references", err)
}
