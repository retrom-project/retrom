package accounts

import (
	"context"
	"fmt"
	"time"

	accountpersistence "retrom/internal/persistence/accounts"
	accountservice "retrom/internal/service/accounts"

	"retrom/internal/authn"
)

func (service *Service) directory() *accountservice.DirectoryService {
	return accountservice.NewDirectory(
		accountpersistence.NewDirectory(
			service.database,
		),
		func() time.Time {
			return service.now()
		},
	)
}

func (service *Service) GetUser(ctx context.Context, id string) (AdminUser, error) {
	result, err := service.directory().Get(ctx, id)
	if err != nil {
		return AdminUser{}, fmt.Errorf("get admin user: %w", err)
	}
	return result, nil
}

func (service *Service) ListUsers(ctx context.Context, filter UserListFilter) ([]AdminUser, error) {
	result, err := service.directory().List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list admin users: %w", err)
	}
	return result, nil
}

func (service *Service) administration() *accountservice.AdministrationService {
	return accountservice.NewAdministration(
		accountpersistence.NewAdministration(
			service.database,
		),
		func() time.Time {
			return service.now()
		},
	)
}

func (service *Service) UpdateUser(
	ctx context.Context,
	principal authn.Principal,
	targetID string,
	version int64,
	patch UserPatch,
	key string,
) (AdminUser, bool, error) {
	user, replayed, err := service.administration().Update(ctx, principal.UserID, targetID, version, patch, key)
	if err != nil {
		return AdminUser{}, false, fmt.Errorf("update user: %w", err)
	}
	return user, replayed, nil
}

func (service *Service) DeleteUser(
	ctx context.Context,
	principal authn.Principal,
	targetID string,
	version int64,
	confirmation, key string,
) (bool, error) {
	replayed, err := service.administration().Delete(ctx, principal.UserID, targetID, version, confirmation, key)
	if err != nil {
		return false, fmt.Errorf("delete user: %w", err)
	}
	return replayed, nil
}
