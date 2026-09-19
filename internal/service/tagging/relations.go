package tagging

import (
	"context"
	"fmt"

	model "retrom/internal/model/tagging"
)

func (service *Service) ValidateReferences(ctx context.Context, scope model.WriteScope, ids []string) (
	[]model.Reference,
	error,
) {
	return ValidateActiveReferences(ctx, scope.Tags, ids)
}

func (service *Service) ReplaceReviewDraftTags(
	ctx context.Context,
	scope model.WriteScope,
	draftID string,
	ids []string,
	actorUserID string,
	now int64,
) ([]model.Reference, []model.Reference, error) {
	if !ValidID(draftID) {
		return nil, nil, model.ErrInvalid
	}
	desired, err := ValidateActiveReferences(ctx, scope.Tags, ids)
	if err != nil {
		return nil, nil, err
	}
	owner := model.Owner{Kind: model.OwnerReviewDraft, ID: draftID}
	before, err := scope.Relations.References(ctx, owner)
	if err != nil {
		return nil, nil, repositoryError("read review tags", err)
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

func applyReplacementPlan(ctx context.Context, scope model.WriteScope, plan model.ReplacementPlan) error {
	if !plan.Changed {
		return nil
	}
	if err := scope.Relations.Remove(ctx, plan.Owner, model.ReferenceIDs(plan.Removed)); err != nil {
		return repositoryError("remove owner tags", err)
	}
	if err := scope.Relations.Add(ctx, model.Assignment{
		Owner: plan.Owner, References: plan.Added, ActorUserID: plan.ActorUserID, NowMS: plan.NowMS,
	}); err != nil {
		return repositoryError("add owner tags", err)
	}
	touched := append(model.ReferenceIDs(plan.Added), model.ReferenceIDs(plan.Removed)...)
	if err := scope.Relations.TouchTags(ctx, plan.ActorUserID, touched, plan.NowMS); err != nil {
		return repositoryError("touch owner tags", err)
	}
	return nil
}

func (service *Service) AssignReviewDraftTags(
	ctx context.Context,
	scope model.WriteScope,
	draftID string,
	refs []model.Reference,
	actorUserID string,
	now int64,
) error {
	if len(refs) == 0 {
		return nil
	}
	if !ValidID(draftID) || !ValidID(actorUserID) {
		return model.ErrInvalid
	}
	if err := scope.Relations.Add(
		ctx, model.Assignment{
			Owner: model.Owner{
				Kind: model.OwnerReviewDraft,
				ID:   draftID,
			},
			References:  refs,
			ActorUserID: actorUserID,
			NowMS:       now,
		},
	); err != nil {
		return repositoryError("assign review tags", err)
	}
	return repositoryError("touch assigned tags", scope.Relations.TouchTags(
		ctx,
		actorUserID,
		model.ReferenceIDs(refs),
		now,
	))
}

func (service *Service) ReviewDraftReferences(
	ctx context.Context,
	scope model.WriteScope,
	draftID string,
) ([]model.Reference, error) {
	return ReviewDraftReferencesInScope(ctx, scope.Relations, draftID)
}

func (service *Service) CopyDraftTagsToGame(
	ctx context.Context,
	scope model.WriteScope,
	draftID, gameID, actorUserID string,
	now int64,
) ([]model.Reference, error) {
	if !ValidID(draftID) || !ValidID(gameID) {
		return nil, model.ErrInvalid
	}
	refs, err := scope.Relations.References(ctx, model.Owner{Kind: model.OwnerReviewDraft, ID: draftID})
	if err != nil {
		return nil, repositoryError("read review tags", err)
	}
	if len(refs) == 0 {
		return refs, nil
	}
	if !ValidID(actorUserID) {
		return nil, model.ErrInvalid
	}
	if err := scope.Relations.Add(
		ctx, model.Assignment{
			Owner: model.Owner{
				Kind: model.OwnerGame,
				ID:   gameID,
			},
			References:  refs,
			ActorUserID: actorUserID,
			NowMS:       now,
		},
	); err != nil {
		return nil, repositoryError("copy draft tags", err)
	}
	if err := scope.Relations.TouchTags(ctx, actorUserID, model.ReferenceIDs(refs), now); err != nil {
		return nil, repositoryError("touch copied tags", err)
	}
	return refs, nil
}

func replaceCollectionTags(
	ctx context.Context,
	scope model.WriteScope,
	owner model.Owner,
	ids []string,
	actorUserID string,
	now int64,
) ([]model.Reference, error) {
	if !ValidID(owner.ID) || !ValidID(actorUserID) {
		return nil, model.ErrInvalid
	}
	desired, err := ValidateActiveReferences(ctx, scope.Tags, ids)
	if err != nil {
		return nil, err
	}
	_, after, err := replaceOwnerReferences(ctx, scope, owner, actorUserID, desired, now)
	return after, err
}

func (service *Service) ReplacePegasusCollectionTags(
	ctx context.Context,
	scope model.WriteScope,
	id string,
	ids []string,
	actorUserID string,
	now int64,
) ([]model.Reference, error) {
	return replaceCollectionTags(ctx, scope, model.Owner{
		Kind: model.OwnerPegasusCollection,
		ID:   id,
	}, ids, actorUserID, now)
}

func (service *Service) ReplaceEmulationStationCollectionTags(
	ctx context.Context,
	scope model.WriteScope,
	id string,
	ids []string,
	actorUserID string,
	now int64,
) ([]model.Reference, error) {
	return replaceCollectionTags(ctx, scope, model.Owner{
		Kind: model.OwnerEmulationStationCollection,
		ID:   id,
	}, ids, actorUserID, now)
}

func (service *Service) PegasusCollectionReferences(
	ctx context.Context,
	scope model.WriteScope,
	id string,
) ([]model.Reference, error) {
	refs, err := scope.Relations.References(ctx, model.Owner{Kind: model.OwnerPegasusCollection, ID: id})
	return refs, repositoryError("collection references", err)
}

func (service *Service) EmulationStationCollectionReferences(
	ctx context.Context,
	scope model.WriteScope,
	id string,
) ([]model.Reference, error) {
	refs, err := scope.Relations.References(ctx, model.Owner{Kind: model.OwnerEmulationStationCollection, ID: id})
	return refs, repositoryError("collection references", err)
}
