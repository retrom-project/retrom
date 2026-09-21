package tagging

import (
	"context"
)

func (service *Service) ReplaceGameTags(
	ctx context.Context,
	actorUserID, gameID string,
	expectedVersion int64,
	tagIDs []string,
) (GameTagResult, error) {
	if !ValidID(actorUserID) || !ValidID(gameID) || expectedVersion < 1 {
		return GameTagResult{}, ErrInvalid
	}
	if _, err := ValidateIDs(tagIDs); err != nil {
		return GameTagResult{}, err
	}
	var result GameTagResult
	err := service.repository.WithWrite(ctx, func(scope WriteScope) error {
		version, err := scope.Games.Version(ctx, gameID)
		if err != nil {
			return repositoryError("read game version", err)
		}
		if version != expectedVersion {
			return ErrVersionConflict
		}
		desired, err := ValidateActiveReferences(ctx, scope.Tags, tagIDs)
		if err != nil {
			return err
		}
		now := service.now().UnixMilli()
		before, after, err := replaceOwnerReferences(
			ctx,
			scope,
			Owner{
				Kind: OwnerGame,
				ID:   gameID,
			},
			actorUserID,
			desired,
			now,
		)
		if err != nil {
			return err
		}
		if sameReferences(before, after) {
			result = GameTagResult{GameID: gameID, Version: version, Tags: before}
			return nil
		}
		if err := scope.Games.Touch(ctx, gameID, expectedVersion, now); err != nil {
			return repositoryError("advance game version", err)
		}
		added, removed := referenceDiff(before, after)
		result = GameTagResult{GameID: gameID, Version: version + 1, Tags: after}
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
