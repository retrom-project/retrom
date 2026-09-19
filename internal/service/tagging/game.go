package tagging

import (
	"context"

	model "retrom/internal/model/tagging"
)

func (service *Service) ReplaceGameTags(
	ctx context.Context,
	actorUserID, gameID string,
	expectedVersion int64,
	tagIDs []string,
) (model.GameTagResult, error) {
	if !ValidID(actorUserID) || !ValidID(gameID) || expectedVersion < 1 {
		return model.GameTagResult{}, model.ErrInvalid
	}
	if _, err := ValidateIDs(tagIDs); err != nil {
		return model.GameTagResult{}, err
	}
	var result model.GameTagResult
	err := service.repository.WithWrite(ctx, func(scope model.WriteScope) error {
		version, err := scope.Games.Version(ctx, gameID)
		if err != nil {
			return repositoryError("read game version", err)
		}
		if version != expectedVersion {
			return model.ErrVersionConflict
		}
		desired, err := ValidateActiveReferences(ctx, scope.Tags, tagIDs)
		if err != nil {
			return err
		}
		now := service.now().UnixMilli()
		before, after, err := replaceOwnerReferences(
			ctx,
			scope, model.Owner{
				Kind: model.OwnerGame,
				ID:   gameID,
			}, actorUserID,
			desired,
			now,
		)
		if err != nil {
			return err
		}
		if model.ReferencesEqual(before, after) {
			result = model.GameTagResult{GameID: gameID, Version: version, Tags: before}
			return nil
		}
		if err := scope.Games.Touch(ctx, gameID, expectedVersion, now); err != nil {
			return repositoryError("advance game version", err)
		}
		added, removed := model.ReferenceDiff(before, after)
		result = model.GameTagResult{GameID: gameID, Version: version + 1, Tags: after}
		return writeAudit(
			ctx,
			scope.Audit,
			actorUserID,
			"GAME_TAGS_REPLACED",
			"GAME",
			gameID,
			before,
			after,
			map[string]any{
				"added":   added,
				"removed": removed,
			},
			now,
		)
	})
	return result, repositoryError("replace game tags", err)
}
