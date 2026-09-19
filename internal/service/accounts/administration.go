package accounts

import (
	"context"
	"encoding/json"
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
	var after model.AdminUser
	var replayed bool
	err = service.repository.WithWrite(ctx, func(scope model.AdministrationScope) error {
		replay, err := scope.Read.Replay(ctx, operation)
		if err != nil {
			return fmt.Errorf("apply account administration: %w", err)
		}
		if err := checkAccountReplay(replay, operation); err != nil {
			return fmt.Errorf("apply account administration: %w", err)
		}
		if replay.Found {
			replayed = true
			if err := json.Unmarshal(replay.Body, &after); err != nil {
				return fmt.Errorf("decode user update replay: %w", err)
			}
			return nil
		}
		var updateErr error
		after, updateErr = updateManagedUser(ctx, scope, operation, targetID, version, patch)
		if updateErr != nil {
			return updateErr
		}

		body, err := json.Marshal(after)
		if err != nil {
			return fmt.Errorf("encode user update replay: %w", err)
		}
		return scope.Write.Remember(ctx, accountReceipt(operation, 200, body))
	})
	if err != nil {
		return model.AdminUser{}, false, fmt.Errorf("update account security: %w", err)
	}
	return after, replayed, nil
}

func anotherAdmin(ctx context.Context, reader model.AdministrationReader, targetID string) error {
	exists, err := reader.AnotherEnabledAdmin(ctx, targetID)
	if err != nil {
		return fmt.Errorf("check remaining enabled administrator: %w", err)
	}
	if !exists {
		return model.ErrLastAdmin
	}
	return nil
}

func auditUserChange(
	ctx context.Context,
	writer model.AdministrationWriter,
	operation model.AccountOperation,
	before, after model.AdminUser,
) error {
	if before.Role != after.Role {
		audit, err := newAccountAudit(
			operation.PrincipalID,
			"USER_ROLE_CHANGED",
			"USER",
			before.UserID,
			map[string]any{
				"role":    before.Role,
				"version": before.Version,
			},
			map[string]any{
				"role":    after.Role,
				"version": after.Version,
			},
			operation.Now,
		)
		if err != nil {
			return fmt.Errorf("apply account administration: %w", err)
		}
		if err := writer.Audit(ctx, audit); err != nil {
			return fmt.Errorf("apply account administration: %w", err)
		}
	}
	if before.Status != after.Status {
		action := "USER_DISABLED"
		if after.Status == "ENABLED" {
			action = "USER_ENABLED"
		}
		audit, err := newAccountAudit(
			operation.PrincipalID,
			action,
			"USER",
			before.UserID,
			map[string]any{
				"status":  before.Status,
				"version": before.Version,
			},
			map[string]any{
				"status":  after.Status,
				"version": after.Version,
			},
			operation.Now,
		)
		if err != nil {
			return fmt.Errorf("apply account administration: %w", err)
		}
		if err := writer.Audit(ctx, audit); err != nil {
			return fmt.Errorf("apply account administration: %w", err)
		}
	}
	return nil
}

func updateManagedUser(
	ctx context.Context,
	scope model.AdministrationScope,
	operation model.AccountOperation,
	targetID string,
	version int64,
	patch model.UserPatch,
) (model.AdminUser, error) {
	before, found, err := scope.Read.Current(ctx, targetID, operation.Now)
	if err != nil {
		return model.AdminUser{}, fmt.Errorf("apply account change: %w", err)
	}
	if err := validateManagedUser(before, found, version); err != nil {
		return model.AdminUser{}, fmt.Errorf("apply account change: %w", err)
	}
	change, err := resolveUserChange(before.User, patch, operation.PrincipalID == targetID)
	if err != nil {
		return model.AdminUser{}, fmt.Errorf("apply account change: %w", err)
	}
	if removesEnabledAdmin(before.User, change.Role, change.Status) {
		if err := anotherAdmin(ctx, scope.Read, targetID); err != nil {
			return model.AdminUser{}, fmt.Errorf("apply account change: %w", err)
		}
	}
	if err := scope.Write.Update(
		ctx, model.AdministrationUpdate{
			Before: before,
			Change: change,
			Now:    operation.Now,
		},
	); err != nil {
		return model.AdminUser{}, fmt.Errorf("apply account change: %w", err)
	}
	current, found, err := scope.Read.Current(ctx, targetID, operation.Now)
	if err != nil {
		return model.AdminUser{}, fmt.Errorf("apply account change: %w", err)
	}
	if !found {
		return model.AdminUser{}, model.ErrUserNotFound
	}
	after := presentAdminUser(current.User)
	if err := auditUserChange(ctx, scope.Write, operation, before.User, after); err != nil {
		return model.AdminUser{}, fmt.Errorf("apply account change: %w", err)
	}
	return after, nil
}
