package accounts

import (
	"context"
	"fmt"

	"retrom/internal/capability/security/authn"

	"github.com/google/uuid"
)

type LinkConsumptionService struct {
	repository LinkConsumptionRepository
	options    LinkConsumptionOptions
}

func NewLinkConsumption(repository LinkConsumptionRepository, options LinkConsumptionOptions) *LinkConsumptionService {
	return &LinkConsumptionService{repository, options}
}

type preparedInvitation struct {
	linkID                  string
	user                    User
	profileID, passwordHash string
	session                 SessionMaterial
}

func (service *LinkConsumptionService) AcceptInvitation(
	ctx context.Context,
	request AcceptInvitationRequest,
) (Session, error) {
	prepared, err := service.prepareInvitation(ctx, request)
	if err != nil {
		return Session{}, err
	}
	var session Session
	err = service.repository.WithConsumptionWrite(ctx, func(scope LinkConsumptionScope) error {
		now := service.options.Now().UnixMilli()
		link, found, err := scope.Read.Current(ctx, prepared.linkID)
		if err != nil {
			return fmt.Errorf("read invitation: %w", err)
		}
		if !activeLink(link, found, "INVITATION", now) || link.Link.Role == nil {
			return ErrAccountLinkUnavailable
		}
		exists, err := scope.Read.UsernameExists(ctx, prepared.user.Username)
		if err != nil {
			return fmt.Errorf("check invited username: %w", err)
		}
		if exists {
			return ErrUsernameUnavailable
		}
		user := prepared.user
		user.Role = *link.Link.Role
		plan := InvitationAcceptance{
			LinkID:       prepared.linkID,
			LinkVersion:  link.Link.Version,
			User:         user,
			ProfileID:    prepared.profileID,
			PasswordHash: prepared.passwordHash,
			Session: prepared.session.Record(
				user.UserID,
				1,
				now,
			),
			Now: now,
		}
		if err := scope.Write.Accept(ctx, plan); err != nil {
			return fmt.Errorf("accept invitation: %w", err)
		}
		audit, err := newAccountAudit(
			user.UserID,
			"INVITATION_ACCEPTED",
			"ACCOUNT_LINK",
			prepared.linkID,
			nil,
			map[string]any{
				"role":   user.Role,
				"status": "CONSUMED",
				"userId": user.UserID,
			},
			now,
		)
		if err != nil {
			return err
		}
		if err := scope.Write.Audit(ctx, audit); err != nil {
			return fmt.Errorf("audit invitation acceptance: %w", err)
		}
		session = prepared.session.View(user, prepared.profileID, 1, now)
		return nil
	})
	if err != nil {
		return Session{}, fmt.Errorf("commit invitation consumption: %w", err)
	}
	return session, nil
}

func (service *LinkConsumptionService) prepareInvitation(
	ctx context.Context,
	request AcceptInvitationRequest,
) (preparedInvitation, error) {
	id, valid := service.options.Tokens.ParseAccountLinkToken("INVITATION", request.Token)
	if !valid {
		return preparedInvitation{}, ErrAccountLinkUnavailable
	}
	username, err := authn.NormalizeUsername(request.Username)
	if err != nil {
		return preparedInvitation{}, fmt.Errorf("normalize invited username: %w", err)
	}
	display, err := authn.NormalizeDisplayName(request.DisplayName)
	if err != nil {
		return preparedInvitation{}, fmt.Errorf("normalize invited display name: %w", err)
	}
	hash, err := service.hashPassword(ctx, request.Password, request.PasswordConfirmation, username, display)
	if err != nil {
		return preparedInvitation{}, err
	}
	session, err := service.options.Mint()
	if err != nil {
		return preparedInvitation{}, fmt.Errorf("prepare invited session: %w", err)
	}
	userID, err := uuid.NewV7()
	if err != nil {
		return preparedInvitation{}, fmt.Errorf("create invited user identity: %w", err)
	}
	profileID, err := uuid.NewV7()
	if err != nil {
		return preparedInvitation{}, fmt.Errorf("create invited profile identity: %w", err)
	}
	return preparedInvitation{
		linkID: id.String(),
		user: User{
			UserID:      userID.String(),
			Username:    username,
			DisplayName: display,
		},
		profileID:    profileID.String(),
		passwordHash: hash,
		session:      session,
	}, nil
}

func (service *LinkConsumptionService) hashPassword(
	ctx context.Context,
	password, confirmation, username, display string,
) (string, error) {
	normalized, err := authn.ValidatePassword(password, confirmation, username, display, service.options.Blocklist)
	if err != nil {
		return "", fmt.Errorf("validate account link password: %w", err)
	}
	hash, err := service.options.Hasher.Hash(ctx, normalized)
	if err != nil {
		return "", fmt.Errorf("hash account link password: %w", err)
	}
	return hash, nil
}

func activeLink(record LinkRecord, found bool, kind string, now int64) bool {
	return found && record.Link.Kind == kind && accountLinkState(record.Link, now) == "ACTIVE"
}
