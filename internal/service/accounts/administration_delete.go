package accounts

import (
	"context"
	"fmt"

	model "retrom/internal/model/accounts"
)

func (service *AdministrationService) Delete(
	ctx context.Context,
	actorID, targetID string,
	version int64,
	confirmation, key string,
) (bool, error) {
	operation, err := newAccountOperation(
		"deleteAdminUser",
		actorID,
		key,
		map[string]any{
			"confirmUsername": confirmation,
			"expectedVersion": version,
			"targetUserId":    targetID,
		},
		service.now().UnixMilli(),
	)
	if err != nil {
		return false, err
	}
	replayed, err := service.repository.CommitDeleteUser(ctx, model.DeleteUserCommand{
		Operation:    operation,
		TargetID:     targetID,
		Version:      version,
		ActorID:      actorID,
		Confirmation: confirmation,
	})
	if err != nil {
		return false, fmt.Errorf("delete account security: %w", err)
	}
	return replayed, nil
}
