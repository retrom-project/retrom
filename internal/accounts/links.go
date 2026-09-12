package accounts

import (
	"context"
	"fmt"
	"time"

	accountpersistence "retrom/internal/persistence/accounts"
	accountservice "retrom/internal/service/accounts"

	"retrom/internal/authn"
)

type (
	AcceptInvitationRequest      = accountservice.AcceptInvitationRequest
	CompletePasswordResetRequest = accountservice.CompletePasswordResetRequest
	PasswordResetResult          = accountservice.PasswordResetResult
)

func (service *Service) issuance() *accountservice.LinkIssuanceService {
	return accountservice.NewLinkIssuance(
		accountpersistence.NewLinks(
			service.database,
		),
		service.credentials,
		func() time.Time {
			return service.now()
		},
	)
}

func (service *Service) CreateInvitation(
	ctx context.Context,
	principal authn.Principal,
	role string,
	confirmed bool,
	key string,
) (AccountLink, bool, error) {
	value, replayed, err := service.issuance().Invitation(
		ctx,
		accountservice.LinkCreator{
			UserID:   principal.UserID,
			Username: principal.Username,
		},
		role,
		confirmed,
		key,
	)
	if err != nil {
		return AccountLink{}, false, fmt.Errorf("create invitation: %w", err)
	}
	return legacyAccountLink(value), replayed, nil
}

func (service *Service) links() *accountservice.LinkService {
	return accountservice.NewLinks(
		accountpersistence.NewLinks(
			service.database,
		),
		service.credentials,
		func() time.Time {
			return service.now()
		},
	)
}

func (service *Service) InspectAccountLink(ctx context.Context, kind, token string) (LinkInspection, error) {
	value, err := service.links().Inspect(ctx, kind, token)
	if err != nil {
		return LinkInspection{}, fmt.Errorf("inspect account link: %w", err)
	}
	result := LinkInspection{Kind: value.Kind, ExpiresAtMS: value.ExpiresAtMS}
	if value.Role != nil {
		result.Role = *value.Role
	}
	if value.Username != nil {
		result.Username = *value.Username
	}
	return result, nil
}

// Invitation consumption must create identity, credential, session, and audit atomically.
func (service *Service) consumption() *accountservice.LinkConsumptionService {
	return accountservice.NewLinkConsumption(
		accountpersistence.NewLinks(
			service.database,
		),
		accountservice.LinkConsumptionOptions{
			Tokens:    service.credentials,
			Hasher:    service.hasher,
			Blocklist: service.blocklist,
			Mint:      service.mintSession,
			Now: func() time.Time {
				return service.now()
			},
		},
	)
}

func (service *Service) AcceptInvitation(ctx context.Context, request AcceptInvitationRequest) (Session, error) {
	result, err := service.consumption().AcceptInvitation(ctx, request)
	if err != nil {
		return Session{}, fmt.Errorf("accept account invitation: %w", err)
	}
	return result, nil
}

func (service *Service) CreatePasswordReset(
	ctx context.Context,
	principal authn.Principal,
	targetID string,
	version int64,
	key string,
) (AccountLink, bool, error) {
	value, replayed, err := service.issuance().PasswordReset(
		ctx,
		accountservice.LinkCreator{
			UserID:   principal.UserID,
			Username: principal.Username,
		},
		targetID,
		version,
		key,
	)
	if err != nil {
		return AccountLink{}, false, fmt.Errorf("create password reset: %w", err)
	}
	return legacyAccountLink(value), replayed, nil
}

// Capability consumption, password rotation, revocation, and optional session are atomic.
func (service *Service) CompletePasswordReset(
	ctx context.Context,
	request CompletePasswordResetRequest,
) (PasswordResetResult, error) {
	result, err := service.consumption().CompleteReset(ctx, request)
	if err != nil {
		return PasswordResetResult{}, fmt.Errorf("complete account password reset: %w", err)
	}
	return result, nil
}

func (service *Service) RevokeAccountLink(
	ctx context.Context,
	principal authn.Principal,
	id string,
	version int64,
	key string,
) (bool, error) {
	replayed, err := service.links().Revoke(ctx, principal.UserID, id, version, key)
	if err != nil {
		return false, fmt.Errorf("revoke account link: %w", err)
	}
	return replayed, nil
}

func (service *Service) ListAccountLinks(ctx context.Context, filter LinkListFilter) ([]AccountLink, error) {
	values, err := service.links().List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list account links: %w", err)
	}
	result := make([]AccountLink, len(values))
	for index, value := range values {
		result[index] = legacyAccountLink(value)
	}
	return result, nil
}

func legacyAccountLink(value accountservice.AccountLink) AccountLink {
	result := AccountLink{
		AccountLinkID:   value.AccountLinkID,
		Kind:            value.Kind,
		State:           value.State,
		Version:         value.Version,
		CreatedAtMS:     value.CreatedAtMS,
		ExpiresAtMS:     value.ExpiresAtMS,
		TargetVersion:   value.TargetVersion,
		CapabilityToken: value.CapabilityToken,
	}
	if value.Role != nil {
		result.Role = *value.Role
	}
	if value.TargetUserID != nil {
		result.TargetUserID = *value.TargetUserID
	}
	if value.CreatedBy != nil {
		result.CreatedBy = map[string]any{"userId": value.CreatedBy.UserID, "username": value.CreatedBy.Username}
	}
	if value.ConsumedAtMS != nil {
		result.ConsumedAtMS = *value.ConsumedAtMS
	}
	if value.RevokedAtMS != nil {
		result.RevokedAtMS = *value.RevokedAtMS
	}
	return result
}
