package accounts

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/accounts"
)

type AdministrationService struct {
	repository model.AdministrationRepository
	now        func() time.Time
}

func NewAdministration(repository model.AdministrationRepository, now func() time.Time) *AdministrationService {
	return &AdministrationService{repository, now}
}

func (service *AdministrationService) Update(
	ctx context.Context,
	actorID, targetID string,
	version int64,
	patch model.UserPatch,
	key string,
) (model.AdminUser, bool, error) {
	operation, err := newAccountOperation(
		"patchAdminUser",
		actorID,
		key,
		map[string]any{
			"confirmAdminRole": patch.ConfirmAdminRole,
			"expectedVersion":  version,
			"role":             patch.Role,
			"status":           patch.Status,
			"targetUserId":     targetID,
		},
		service.now().UnixMilli(),
	)
	if err != nil {
		return model.AdminUser{}, false, err
	}
	result, err := service.repository.CommitUpdateUser(ctx, model.UpdateUserCommand{
		Operation: operation,
		TargetID:  targetID,
		Version:   version,
		Patch:     patch,
	})
	if err != nil {
		return model.AdminUser{}, false, fmt.Errorf("update account security: %w", err)
	}
	return result.User, result.Replayed, nil
}
