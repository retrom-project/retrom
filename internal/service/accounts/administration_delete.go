package accounts

import (
	"context"
	"fmt"
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
	var replayed bool
	err = service.repository.WithWrite(ctx, func(scope AdministrationScope) error {
		replay, err := scope.Read.Replay(ctx, operation)
		if err != nil {
			return fmt.Errorf("apply account administration: %w", err)
		}
		if err := checkAccountReplay(replay, operation); err != nil {
			return fmt.Errorf("apply account administration: %w", err)
		}
		if replay.Found {
			replayed = true
			return nil
		}
		before, found, err := scope.Read.Current(ctx, targetID, operation.Now)
		if err != nil {
			return fmt.Errorf("apply account administration: %w", err)
		}
		if err := validateManagedUser(before, found, version); err != nil {
			return fmt.Errorf("apply account administration: %w", err)
		}
		if err := validateUserDeletion(before.User, actorID, confirmation); err != nil {
			return fmt.Errorf("apply account administration: %w", err)
		}
		if removesEnabledAdmin(before.User, before.User.Role, "DELETED") {
			if err := anotherAdmin(ctx, scope.Read, targetID); err != nil {
				return fmt.Errorf("apply account administration: %w", err)
			}
		}
		plan := AdministrationDeletion{
			Before: before,
			Security: UserSecurity{
				Reason:       "USER_DELETED",
				Sessions:     true,
				CreatedLinks: true,
				TargetLinks:  true,
				Launches:     true,
			},
			ClearTestDefault: before.User.Username == "test",
			Now:              operation.Now,
		}
		if err := scope.Write.Delete(ctx, plan); err != nil {
			return fmt.Errorf("apply account administration: %w", err)
		}
		audit, err := newAccountAudit(
			actorID,
			"USER_DELETED",
			"USER",
			targetID,
			map[string]any{
				"role":    before.User.Role,
				"status":  before.User.Status,
				"version": before.User.Version,
			},
			map[string]any{
				"role":    before.User.Role,
				"status":  "DELETED",
				"version": before.User.Version + 1,
			},
			operation.Now,
		)
		if err != nil {
			return fmt.Errorf("apply account administration: %w", err)
		}
		if err := scope.Write.Audit(ctx, audit); err != nil {
			return fmt.Errorf("apply account administration: %w", err)
		}
		return scope.Write.Remember(ctx, accountReceipt(operation, 204, nil))
	})
	if err != nil {
		return false, fmt.Errorf("delete account security: %w", err)
	}
	return replayed, nil
}
