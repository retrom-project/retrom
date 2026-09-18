package accounts

import (
	"context"
	"fmt"

	model "retrom/internal/model/accounts"

	"retrom/internal/capability/security/authn"

	"github.com/google/uuid"
)

type LinkConsumptionService struct {
	repository model.LinkConsumptionRepository
	options    model.LinkConsumptionOptions
}

func NewLinkConsumption(
	repository model.LinkConsumptionRepository,
	options model.LinkConsumptionOptions,
) *LinkConsumptionService {
	return &LinkConsumptionService{repository, options}
}

type preparedInvitation struct {
	linkID                  string
	user                    model.User
	profileID, passwordHash string
	session                 model.SessionMaterial
}

func (service *LinkConsumptionService) AcceptInvitation(
	ctx context.Context,
	request model.AcceptInvitationRequest,
) (model.Session, error) {
	prepared, err := service.prepareInvitation(ctx, request)
	if err != nil {
		return model.Session{}, err
	}
	now := service.options.Now().UnixMilli()
	link, found, err := service.repository.LoadInvitationLink(ctx, prepared.linkID)
	if err != nil {
		return model.Session{}, fmt.Errorf("read invitation: %w", err)
	}
	if !model.ActiveLink(link, found, "INVITATION", now) || link.Link.Role == nil {
		return model.Session{}, model.ErrAccountLinkUnavailable
	}
	user := prepared.user
	user.Role = *link.Link.Role
	plan := model.InvitationAcceptance{
		LinkID:       prepared.linkID,
		LinkVersion:  link.Link.Version,
		User:         user,
		ProfileID:    prepared.profileID,
		PasswordHash: prepared.passwordHash,
		Session:      prepared.session.Record(user.UserID, 1, now),
		Now:          now,
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
		return model.Session{}, err
	}
	if err := service.repository.CommitInvitationAcceptance(
		ctx, model.InvitationAcceptCommand{Plan: plan, Audit: audit},
	); err != nil {
		return model.Session{}, fmt.Errorf("commit invitation consumption: %w", err)
	}
	return prepared.session.View(user, prepared.profileID, 1, now), nil
}

func (service *LinkConsumptionService) prepareInvitation(
	ctx context.Context,
	request model.AcceptInvitationRequest,
) (preparedInvitation, error) {
	id, valid := service.options.Tokens.ParseAccountLinkToken("INVITATION", request.Token)
	if !valid {
		return preparedInvitation{}, model.ErrAccountLinkUnavailable
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
		user: model.User{
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
