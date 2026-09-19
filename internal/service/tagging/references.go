package tagging

import (
	"context"
	"fmt"

	model "retrom/internal/model/tagging"
)

func ValidateActiveReferences(ctx context.Context, reader model.TagReader, tagIDs []string) ([]model.Reference, error) {
	validated, err := ValidateIDs(tagIDs)
	if err != nil {
		return nil, err
	}
	if len(validated) == 0 {
		return []model.Reference{}, nil
	}
	result, err := reader.ActiveReferences(ctx, validated)
	if err != nil {
		return nil, repositoryError("read active references", err)
	}
	result, err = model.ValidateActiveReferenceFacts(validated, result)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}
	return result, nil
}

func replaceOwnerReferences(
	ctx context.Context,
	scope model.WriteScope,
	owner model.Owner,
	actorUserID string,
	desired []model.Reference,
	now int64,
) ([]model.Reference, []model.Reference, error) {
	before, err := scope.Relations.References(ctx, owner)
	if err != nil {
		return nil, nil, repositoryError("read owner references", err)
	}
	plan, err := model.BuildReplacementPlan(owner, before, desired, actorUserID, now)
	if err != nil {
		return nil, nil, fmt.Errorf("%w", err)
	}
	if err := applyReplacementPlan(ctx, scope, plan); err != nil {
		return nil, nil, err
	}
	return plan.Before, plan.After, nil
}
