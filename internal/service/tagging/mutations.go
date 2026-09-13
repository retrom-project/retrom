package tagging

import (
	"context"
)

func (service *Service) Create(ctx context.Context, actorUserID, rawName string) (AdminItem, error) {
	if !ValidID(actorUserID) {
		return AdminItem{}, ErrInvalid
	}
	name, key, search, err := NormalizeName(rawName)
	if err != nil {
		return AdminItem{}, err
	}
	var result AdminItem
	err = service.repository.WithWrite(ctx, func(scope WriteScope) error {
		active, err := scope.Tags.ActiveByNameKey(ctx)
		if err != nil {
			return repositoryError("read active tags", err)
		}
		if len(active) >= MaxActiveTags {
			return ErrLimitReached
		}
		if active[key] != "" {
			return ErrNameConflict
		}
		result, err = createTag(
			ctx,
			scope,
			actorUserID,
			normalizedCommonTag{
				name:       name,
				nameKey:    key,
				searchText: search,
			},
			service.now().UnixMilli(),
		)
		return err
	})
	return result, repositoryError("create", err)
}

func (service *Service) Rename(
	ctx context.Context,
	actorUserID, tagID, rawName string,
	expectedVersion int64,
) (AdminItem, error) {
	if !ValidID(actorUserID) || !ValidID(tagID) || expectedVersion < 1 {
		return AdminItem{}, ErrInvalid
	}
	name, key, search, err := NormalizeName(rawName)
	if err != nil {
		return AdminItem{}, err
	}
	var result AdminItem
	err = service.repository.WithWrite(ctx, func(scope WriteScope) error {
		before, err := scope.Tags.Get(ctx, tagID)
		if err != nil {
			return repositoryError("read tag", err)
		}
		if before.Status == StatusDeleted {
			return ErrAlreadyDeleted
		}
		if before.Version != expectedVersion {
			return ErrVersionConflict
		}
		if before.Name == name {
			return ErrInvalid
		}
		active, err := scope.Tags.ActiveByNameKey(ctx)
		if err != nil {
			return repositoryError("read active tags", err)
		}
		if active[key] != "" && active[key] != tagID {
			return ErrNameConflict
		}
		now := service.now().UnixMilli()
		if err := scope.Changes.Rename(
			ctx,
			TagWrite{
				ID:              tagID,
				Name:            name,
				NameKey:         key,
				SearchText:      search,
				ActorUserID:     actorUserID,
				ExpectedVersion: expectedVersion,
				NowMS:           now,
			},
		); err != nil {
			return repositoryError("rename tag", err)
		}
		result, err = scope.Tags.Get(ctx, tagID)
		if err != nil {
			return repositoryError("read renamed tag", err)
		}
		return writeAudit(
			ctx,
			scope.Audit,
			actorUserID,
			"TAG_RENAMED",
			"TAG",
			tagID,
			before,
			result,
			map[string]any{
				"name": map[string]string{
					"before": before.Name,
					"after":  result.Name,
				},
			},
			now,
		)
	})
	return result, repositoryError("rename", err)
}

func (service *Service) Delete(
	ctx context.Context,
	actorUserID, tagID, confirmName string,
	expectedVersion int64,
) (AdminItem, DeleteImpact, error) {
	if !ValidID(actorUserID) || !ValidID(tagID) || expectedVersion < 1 {
		return AdminItem{}, DeleteImpact{}, ErrInvalid
	}
	var result AdminItem
	var impact DeleteImpact
	err := service.repository.WithWrite(ctx, func(scope WriteScope) error {
		before, err := scope.Tags.Get(ctx, tagID)
		if err != nil {
			return repositoryError("read tag", err)
		}
		if before.Status == StatusDeleted {
			return ErrAlreadyDeleted
		}
		if before.Version != expectedVersion {
			return ErrVersionConflict
		}
		if confirmName != before.Name {
			return ErrDeleteConfirmation
		}
		impact = DeleteImpact(before.Usage)
		now := service.now().UnixMilli()
		if err := scope.Changes.Delete(
			ctx,
			TagWrite{
				ID:              tagID,
				ActorUserID:     actorUserID,
				ExpectedVersion: expectedVersion,
				NowMS:           now,
			},
		); err != nil {
			return repositoryError("delete tag", err)
		}
		result, err = scope.Tags.Get(ctx, tagID)
		if err != nil {
			return repositoryError("read deleted tag", err)
		}
		return writeAudit(
			ctx,
			scope.Audit,
			actorUserID,
			"TAG_DELETED",
			"TAG",
			tagID,
			before,
			result,
			map[string]any{
				"impact": impact,
			},
			now,
		)
	})
	return result, impact, repositoryError("delete", err)
}
