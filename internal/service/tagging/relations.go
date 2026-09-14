package tagging

import (
	"context"
)

func (service *Service) ValidateReferences(ctx context.Context, scope WriteScope, ids []string) ([]Reference, error) {
	return ValidateActiveReferences(ctx, scope.Tags, ids)
}

func (service *Service) ReplaceReviewDraftTags(
	ctx context.Context,
	scope WriteScope,
	draftID string,
	ids []string,
	actorUserID string,
	now int64,
) ([]Reference, []Reference, error) {
	if !ValidID(draftID) {
		return nil, nil, ErrInvalid
	}
	desired, err := ValidateActiveReferences(ctx, scope.Tags, ids)
	if err != nil {
		return nil, nil, err
	}
	owner := Owner{Kind: OwnerReviewDraft, ID: draftID}
	before, err := scope.Relations.References(ctx, owner)
	if err != nil {
		return nil, nil, repositoryError("read review tags", err)
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

func applyReplacementPlan(ctx context.Context, scope WriteScope, plan ReplacementPlan) error {
	if !plan.Changed {
		return nil
	}
	if err := scope.Relations.Remove(ctx, plan.Owner, ReferenceIDs(plan.Removed)); err != nil {
		return repositoryError("remove owner tags", err)
	}
	if err := scope.Relations.Add(ctx, Assignment{
		Owner: plan.Owner, References: plan.Added, ActorUserID: plan.ActorUserID, NowMS: plan.NowMS,
	}); err != nil {
		return repositoryError("add owner tags", err)
	}
	touched := append(ReferenceIDs(plan.Added), ReferenceIDs(plan.Removed)...)
	if err := scope.Relations.TouchTags(ctx, plan.ActorUserID, touched, plan.NowMS); err != nil {
		return repositoryError("touch owner tags", err)
	}
	return nil
}

func (service *Service) AssignReviewDraftTags(
	ctx context.Context,
	scope WriteScope,
	draftID string,
	refs []Reference,
	actorUserID string,
	now int64,
) error {
	if len(refs) == 0 {
		return nil
	}
	if !ValidID(draftID) || !ValidID(actorUserID) {
		return ErrInvalid
	}
	if err := scope.Relations.Add(
		ctx,
		Assignment{
			Owner: Owner{
				Kind: OwnerReviewDraft,
				ID:   draftID,
			},
			References:  refs,
			ActorUserID: actorUserID,
			NowMS:       now,
		},
	); err != nil {
		return repositoryError("assign review tags", err)
	}
	return repositoryError("touch assigned tags", scope.Relations.TouchTags(ctx, actorUserID, ReferenceIDs(refs), now))
}

func (service *Service) ReviewDraftReferences(
	ctx context.Context,
	scope WriteScope,
	draftID string,
) ([]Reference, error) {
	return ReviewDraftReferencesInScope(ctx, scope.Relations, draftID)
}

func (service *Service) CopyDraftTagsToGame(
	ctx context.Context,
	scope WriteScope,
	draftID, gameID, actorUserID string,
	now int64,
) ([]Reference, error) {
	if !ValidID(draftID) || !ValidID(gameID) {
		return nil, ErrInvalid
	}
	refs, err := scope.Relations.References(ctx, Owner{Kind: OwnerReviewDraft, ID: draftID})
	if err != nil {
		return nil, repositoryError("read review tags", err)
	}
	if len(refs) == 0 {
		return refs, nil
	}
	if !ValidID(actorUserID) {
		return nil, ErrInvalid
	}
	if err := scope.Relations.Add(
		ctx,
		Assignment{
			Owner: Owner{
				Kind: OwnerGame,
				ID:   gameID,
			},
			References:  refs,
			ActorUserID: actorUserID,
			NowMS:       now,
		},
	); err != nil {
		return nil, repositoryError("copy draft tags", err)
	}
	if err := scope.Relations.TouchTags(ctx, actorUserID, ReferenceIDs(refs), now); err != nil {
		return nil, repositoryError("touch copied tags", err)
	}
	return refs, nil
}

func replaceCollectionTags(
	ctx context.Context,
	scope WriteScope,
	owner Owner,
	ids []string,
	actorUserID string,
	now int64,
) ([]Reference, error) {
	if !ValidID(owner.ID) || !ValidID(actorUserID) {
		return nil, ErrInvalid
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
	scope WriteScope,
	id string,
	ids []string,
	actorUserID string,
	now int64,
) ([]Reference, error) {
	return replaceCollectionTags(ctx, scope, Owner{Kind: OwnerPegasusCollection, ID: id}, ids, actorUserID, now)
}

func (service *Service) ReplaceEmulationStationCollectionTags(
	ctx context.Context,
	scope WriteScope,
	id string,
	ids []string,
	actorUserID string,
	now int64,
) ([]Reference, error) {
	return replaceCollectionTags(ctx, scope, Owner{Kind: OwnerEmulationStationCollection, ID: id}, ids, actorUserID, now)
}

func (service *Service) PegasusCollectionReferences(
	ctx context.Context,
	scope WriteScope,
	id string,
) ([]Reference, error) {
	refs, err := scope.Relations.References(ctx, Owner{Kind: OwnerPegasusCollection, ID: id})
	return refs, repositoryError("collection references", err)
}

func (service *Service) EmulationStationCollectionReferences(
	ctx context.Context,
	scope WriteScope,
	id string,
) ([]Reference, error) {
	refs, err := scope.Relations.References(ctx, Owner{Kind: OwnerEmulationStationCollection, ID: id})
	return refs, repositoryError("collection references", err)
}
