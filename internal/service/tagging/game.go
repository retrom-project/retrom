package tagging

import (
	"context"
	"fmt"

	model "retrom/internal/model/tagging"
)

func (service *Service) ReplaceGameTags(
	ctx context.Context,
	actorUserID, gameID string,
	expectedVersion int64,
	tagIDs []string,
) (model.GameTagResult, error) {
	if !model.ValidID(actorUserID) || !model.ValidID(gameID) || expectedVersion < 1 {
		return model.GameTagResult{}, model.ErrInvalid
	}
	if _, err := model.ValidateIDs(tagIDs); err != nil {
		return model.GameTagResult{}, fmt.Errorf("validate tag IDs: %w", err)
	}
	auditID, err := service.newID()
	if err != nil {
		return model.GameTagResult{}, repositoryError("game tags audit id", err)
	}
	result, err := service.commands.CommitReplaceGameTags(ctx, model.ReplaceGameTagsCommand{
		GameID: gameID, AuditID: auditID, ActorUserID: actorUserID,
		ExpectedVersion: expectedVersion, NowMS: service.now().UnixMilli(),
		TagIDs: tagIDs,
	})
	return result, repositoryError("replace game tags", err)
}
