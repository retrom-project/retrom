package accounts

import (
	"context"
	"fmt"
	"time"

	"retrom/internal/capability/security/authn"
	model "retrom/internal/model/accounts"

	"github.com/google/uuid"
)

type RecoveryService struct {
	repository model.RecoveryRepository
	hasher     model.PasswordHasher
	blocklist  *authn.Blocklist
	now        func() time.Time
}

func NewRecovery(
	repository model.RecoveryRepository,
	hasher model.PasswordHasher,
	blocklist *authn.Blocklist,
	now func() time.Time,
) *RecoveryService {
	return &RecoveryService{repository: repository, hasher: hasher, blocklist: blocklist, now: now}
}

func (service *RecoveryService) Reset(ctx context.Context, username, password, confirmation string) error {
	target, hash, err := service.prepare(ctx, username, password, confirmation)
	if err != nil {
		return err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("create recovery audit identity: %w", err)
	}
	err = service.repository.WithWrite(ctx, func(scope model.RecoveryScope) error {
		current, found, err := scope.Read.Current(ctx, target.UserID)
		if err != nil {
			return fmt.Errorf("recheck offline recovery target: %w", err)
		}
		if !found || !recoverable(current) || current.Version != target.Version {
			return model.ErrOfflineAdmin
		}
		plan := model.RecoveryPlan{
			Target: current, PasswordHash: hash, AuditID: id.String(),
			ClearTestDefault: current.Username == "test", Now: service.now().UnixMilli(),
			BeforeJSON: fmt.Sprintf(
				`{"status":%q,"version":%d}`,
				current.Status,
				current.Version,
			), AfterJSON: fmt.Sprintf(
				`{"status":"ENABLED","version":%d}`,
				current.Version+1,
			),
		}
		if err := scope.Write.Reset(ctx, plan); err != nil {
			return fmt.Errorf("reset offline admin security: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit offline admin recovery: %w", err)
	}
	return nil
}

func (service *RecoveryService) prepare(
	ctx context.Context,
	usernameInput, password, confirmation string,
) (model.RecoveryTarget, string, error) {
	username, err := authn.NormalizeUsername(usernameInput)
	if err != nil {
		return model.RecoveryTarget{}, "", model.ErrOfflineAdmin
	}
	target, found, err := service.repository.ByUsername(ctx, username)
	if err != nil {
		return target, "", fmt.Errorf("read recovery target: %w", err)
	}
	if !found || !recoverable(target) {
		return target, "", model.ErrOfflineAdmin
	}
	normalized, err := authn.ValidatePassword(
		password,
		confirmation,
		target.Username,
		target.DisplayName,
		service.blocklist,
	)
	if err != nil {
		return target, "", fmt.Errorf("validate offline recovery password: %w", err)
	}
	hash, err := service.hasher.Hash(ctx, normalized)
	if err != nil {
		return target, "", fmt.Errorf("hash offline recovery password: %w", err)
	}
	return target, hash, nil
}

func recoverable(target model.RecoveryTarget) bool {
	return target.Role == "ADMIN" && target.Status != "DELETED"
}
