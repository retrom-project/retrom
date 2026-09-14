package tagging

import (
	"context"
)

func ValidateActiveReferences(ctx context.Context, reader TagReader, tagIDs []string) ([]Reference, error) {
	validated, err := ValidateIDs(tagIDs)
	if err != nil {
		return nil, err
	}
	if len(validated) == 0 {
		return []Reference{}, nil
	}
	result, err := reader.ActiveReferences(ctx, validated)
	if err != nil {
		return nil, repositoryError("read active references", err)
	}
	return ValidateActiveReferenceFacts(validated, result)
}

func replaceOwnerReferences(
	ctx context.Context,
	scope WriteScope,
	owner Owner,
	actorUserID string,
	desired []Reference,
	now int64,
) ([]Reference, []Reference, error) {
	before, err := scope.Relations.References(ctx, owner)
	if err != nil {
		return nil, nil, repositoryError("read owner references", err)
	}
	plan, err := BuildReplacementPlan(owner, before, desired, actorUserID, now)
	if err != nil {
		return nil, nil, err
	}
	if err := applyReplacementPlan(ctx, scope, plan); err != nil {
		return nil, nil, err
	}
	return plan.Before, plan.After, nil
}
