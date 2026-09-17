package accounts

import (
	"context"

	model "retrom/internal/model/accounts"

	"retrom/internal/capability/security/authn"
)

func (service *Service) GetUser(ctx context.Context, id string) (model.AdminUser, error) {
	return service.modules.Directory.Get(ctx, id)
}

func (service *Service) ListUsers(ctx context.Context, filter model.UserListFilter) ([]model.AdminUser, error) {
	return service.modules.Directory.List(ctx, filter)
}

func (service *Service) UpdateUser(
	ctx context.Context,
	principal authn.Principal,
	targetID string,
	version int64,
	patch model.UserPatch,
	key string,
) (model.AdminUser, bool, error) {
	return service.modules.Administration.Update(ctx, principal.UserID, targetID, version, patch, key)
}

func (service *Service) DeleteUser(
	ctx context.Context,
	principal authn.Principal,
	targetID string,
	version int64,
	confirmation, key string,
) (bool, error) {
	return service.modules.Administration.Delete(ctx, principal.UserID, targetID, version, confirmation, key)
}
