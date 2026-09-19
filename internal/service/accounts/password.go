package accounts

import (
	"context"
	"fmt"
	"time"

	"retrom/internal/capability/security/authn"
	model "retrom/internal/model/accounts"

	"github.com/google/uuid"
)

type PasswordService struct {
	repository model.PasswordRepository
	hasher     model.PasswordHasher
	blocklist  *authn.Blocklist
	mint       model.SessionMinter
	now        func() time.Time
}

func NewPasswords(
	repository model.PasswordRepository,
	hasher model.PasswordHasher,
	blocklist *authn.Blocklist,
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
	var result model.Session
	err = service.repository.WithWrite(ctx, func(scope model.PasswordScope) error {
		now := service.now().UnixMilli()
		state, found, err := scope.Read.Current(ctx, actor, now)
		if err != nil {
			return fmt.Errorf("recheck password authorization: %w", err)
		}
		if !found || !passwordAuthorized(
			state,
			actor,
		) || state.Credential.PasswordHash != prepared.state.Credential.PasswordHash {
			return model.ErrAuthenticationNeeded
		}
		version := state.Credential.SessionVersion + 1
		plan := model.PasswordPlan{
			Actor:        actor,
			ExpectedHash: prepared.state.Credential.PasswordHash, NewHash: prepared.newHash, AuditID: prepared.auditID,
			Session: prepared.session.Record(
				actor.UserID,
				version,
				now,
			), BeforeJSON: fmt.Sprintf(
				`{"sessionVersion":%d}`,
				version-1,
			), AfterJSON: fmt.Sprintf(
				`{"sessionVersion":%d}`,
				version,
			), ClearTestDefault: state.Credential.User.Username == "test", Now: now,
		}
		if err := scope.Write.Rotate(ctx, plan); err != nil {
			return fmt.Errorf("rotate password security state: %w", err)
		}
		result = prepared.session.View(state.Credential.User, state.Credential.ProfileID, version, now)
		return nil
	})
	if err != nil {
		return model.Session{}, fmt.Errorf("commit password change: %w", err)
	}
	return result, nil
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
	if !found || !passwordAuthorized(state, actor) {
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

func passwordAuthorized(state model.PasswordState, actor model.PasswordActor) bool {
	return state.SessionCurrent && state.Credential.Status == "ENABLED" &&
		state.Credential.SessionVersion == actor.SessionVersion
}
