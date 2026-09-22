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
	if sameReferences(before, desired) {
		return before, desired, nil
	}
	if !ValidID(actorUserID) {
		return nil, nil, ErrInvalid
	}
	return replaceOwnerReferences(ctx, scope, owner, actorUserID, desired, now)
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
	return repositoryError("touch assigned tags", scope.Relations.TouchTags(ctx, actorUserID, referenceIDs(refs), now))
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
	if err := scope.Relations.TouchTags(ctx, actorUserID, referenceIDs(refs), now); err != nil {
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

func (service *Service) ReplaceSourceCollectionTags(
	ctx context.Context,
	scope WriteScope,
	id string,
	ids []string,
	actorUserID string,
	now int64,
) ([]Reference, error) {
	return replaceCollectionTags(ctx, scope, Owner{Kind: OwnerSourceCollection, ID: id}, ids, actorUserID, now)
}

func (service *Service) SourceCollectionReferences(
	ctx context.Context,
	scope WriteScope,
	id string,
) ([]Reference, error) {
	refs, err := scope.Relations.References(ctx, Owner{Kind: OwnerSourceCollection, ID: id})
	return refs, repositoryError("collection references", err)
}
