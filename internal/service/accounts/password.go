package accounts

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/accounts"

	"retrom/internal/capability/security/authn"

	"github.com/google/uuid"
)

type PasswordService struct {
	repository model.PasswordRepository
	hasher     model.PasswordHasher
	blocklist  authn.Blocklist
	mint       model.SessionMinter
	now        func() time.Time
}

func NewPasswords(
	repository model.PasswordRepository,
	hasher model.PasswordHasher,
	blocklist authn.Blocklist,
	mint model.SessionMinter,
	now func() time.Time,
) *PasswordService {
	return &PasswordService{repository: repository, hasher: hasher, blocklist: blocklist, mint: mint, now: now}
}

type preparedPassword struct {
	state   model.PasswordState
	newHash string
	session model.SessionMaterial
	auditID string
}

func (service *PasswordService) Change(
	ctx context.Context,
	actor model.PasswordActor,
	current, password, confirmation string,
) (model.Session, error) {
	prepared, err := service.prepare(ctx, actor, current, password, confirmation)
	if err != nil {
		return model.Session{}, err
	}
	now := service.now().UnixMilli()
	version := actor.SessionVersion + 1
	result, err := service.repository.CommitChangePassword(ctx, model.ChangePasswordCommand{
		Actor:        actor,
		ExpectedHash: prepared.state.Credential.PasswordHash,
		NewHash:      prepared.newHash,
		AuditID:      prepared.auditID,
		Session:      prepared.session.Record(actor.UserID, version, now),
		NowMS:        now,
	})
	if err != nil {
		return model.Session{}, fmt.Errorf("commit password change: %w", err)
	}
	return prepared.session.View(result.User, result.ProfileID, result.Version, now), nil
}

func (service *PasswordService) prepare(
	ctx context.Context,
	actor model.PasswordActor,
	current, password, confirmation string,
) (preparedPassword, error) {
	normalized, err := authn.NormalizeLoginPassword(current)
	if err != nil {
		return preparedPassword{}, model.ErrAuthentication
	}
	state, found, err := service.repository.Current(ctx, actor, service.now().UnixMilli())
	if err != nil {
		return preparedPassword{}, fmt.Errorf("read password authorization: %w", err)
	}
	if !found || !model.PasswordAuthorized(state, actor) {
		return preparedPassword{}, model.ErrAuthenticationNeeded
	}
	verified, err := service.hasher.Verify(ctx, normalized, state.Credential.PasswordHash)
	if err != nil {
		return preparedPassword{}, fmt.Errorf("verify current password: %w", err)
	}
	if !verified {
		return preparedPassword{}, model.ErrAuthentication
	}
	validated, err := authn.ValidatePassword(
		password,
		confirmation,
		state.Credential.User.Username,
		state.Credential.User.DisplayName,
		service.blocklist,
	)
	if err != nil {
		return preparedPassword{}, fmt.Errorf("validate replacement password: %w", err)
	}
	hash, err := service.hasher.Hash(ctx, validated)
	if err != nil {
		return preparedPassword{}, fmt.Errorf("hash replacement password: %w", err)
	}
	material, err := service.mint()
	if err != nil {
		return preparedPassword{}, fmt.Errorf("prepare replacement session: %w", err)
	}
	auditID, err := uuid.NewV7()
	if err != nil {
		return preparedPassword{}, fmt.Errorf("create password audit identity: %w", err)
	}
	return preparedPassword{state: state, newHash: hash, session: material, auditID: auditID.String()}, nil
}
