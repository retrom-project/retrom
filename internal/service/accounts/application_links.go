package accounts

import (
	"context"

	"retrom/internal/capability/security/authn"
)

func (service *Service) CreateInvitation(
	ctx context.Context,
	principal authn.Principal,
	role string,
	confirmed bool,
	key string,
) (AccountLink, bool, error) {
	return service.modules.Issuance.Invitation(
		ctx,
		LinkCreator{
			UserID:   principal.UserID,
			Username: principal.Username,
		},
		role,
		confirmed,
		key,
	)
}

func (service *Service) CreatePasswordReset(
	ctx context.Context,
	principal authn.Principal,
	targetID string,
	version int64,
	key string,
) (AccountLink, bool, error) {
	return service.modules.Issuance.PasswordReset(
		ctx,
		LinkCreator{
			UserID:   principal.UserID,
			Username: principal.Username,
		},
		targetID,
		version,
		key,
	)
}

func (service *Service) InspectAccountLink(ctx context.Context, kind, token string) (LinkInspection, error) {
	return service.modules.Links.Inspect(ctx, kind, token)
}

func (service *Service) AcceptInvitation(ctx context.Context, request AcceptInvitationRequest) (Session, error) {
	return service.modules.Consumption.AcceptInvitation(ctx, request)
}

func (service *Service) CompletePasswordReset(
	ctx context.Context,
	request CompletePasswordResetRequest,
) (PasswordResetResult, error) {
	return service.modules.Consumption.CompleteReset(ctx, request)
}

func (service *Service) RevokeAccountLink(
	ctx context.Context,
	principal authn.Principal,
	id string,
	version int64,
	key string,
) (bool, error) {
	return service.modules.Links.Revoke(ctx, principal.UserID, id, version, key)
}

func (service *Service) ListAccountLinks(ctx context.Context, filter LinkListFilter) ([]AccountLink, error) {
	return service.modules.Links.List(ctx, filter)
}
