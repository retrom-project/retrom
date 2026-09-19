package tagging

import (
	"context"

	model "retrom/internal/model/tagging"
)

func (service *Service) Create(ctx context.Context, actorUserID, rawName string) (model.AdminItem, error) {
	if !ValidID(actorUserID) {
		return model.AdminItem{}, model.ErrInvalid
	}
	name, key, search, err := NormalizeName(rawName)
	if err != nil {
		return model.AdminItem{}, err
	}
	var result model.AdminItem
	err = service.repository.WithWrite(ctx, func(scope model.WriteScope) error {
		active, err := scope.Tags.ActiveByNameKey(ctx)
		if err != nil {
			return repositoryError("read active tags", err)
		}
		if len(active) >= model.MaxActiveTags {
			return model.ErrLimitReached
		}
		if active[key] != "" {
			return model.ErrNameConflict
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
) (model.AdminItem, error) {
	if !ValidID(actorUserID) || !ValidID(tagID) || expectedVersion < 1 {
		return model.AdminItem{}, model.ErrInvalid
	}
	name, key, search, err := NormalizeName(rawName)
	if err != nil {
		return model.AdminItem{}, err
	}
	var result model.AdminItem
	err = service.repository.WithWrite(ctx, func(scope model.WriteScope) error {
		before, err := scope.Tags.Get(ctx, tagID)
		if err != nil {
			return repositoryError("read tag", err)
		}
		if before.Status == model.StatusDeleted {
			return model.ErrAlreadyDeleted
		}
		if before.Version != expectedVersion {
			return model.ErrVersionConflict
		}
		if before.Name == name {
			return model.ErrInvalid
		}
		active, err := scope.Tags.ActiveByNameKey(ctx)
		if err != nil {
			return repositoryError("read active tags", err)
		}
		if active[key] != "" && active[key] != tagID {
			return model.ErrNameConflict
		}
		now := service.now().UnixMilli()
		if err := scope.Changes.Rename(
			ctx, model.TagWrite{
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
) (model.AdminItem, model.DeleteImpact, error) {
	if !ValidID(actorUserID) || !ValidID(tagID) || expectedVersion < 1 {
		return model.AdminItem{}, model.DeleteImpact{}, model.ErrInvalid
	}
	var result model.AdminItem
	var impact model.DeleteImpact
	err := service.repository.WithWrite(ctx, func(scope model.WriteScope) error {
		before, err := scope.Tags.Get(ctx, tagID)
		if err != nil {
			return repositoryError("read tag", err)
		}
		if before.Status == model.StatusDeleted {
			return model.ErrAlreadyDeleted
		}
		if before.Version != expectedVersion {
			return model.ErrVersionConflict
		}
		if confirmName != before.Name {
			return model.ErrDeleteConfirmation
		}
		impact = model.DeleteImpact(before.Usage)
		now := service.now().UnixMilli()
		if err := scope.Changes.Delete(
			ctx, model.TagWrite{
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
