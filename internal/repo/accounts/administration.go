package accounts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"retrom/internal/model/accounts"
	"retrom/internal/repo/dbexec"

	"github.com/google/uuid"
)

type (
	AdministrationRepository struct{ database *sql.DB }
	administrationRecords    struct{ accountOperations }
)

func NewAdministration(database *sql.DB) *AdministrationRepository {
	return &AdministrationRepository{database}
}

func (repository *AdministrationRepository) CommitUpdateUser(
	ctx context.Context, cmd accounts.UpdateUserCommand,
) (accounts.UpdateUserResult, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return accounts.UpdateUserResult{}, fmt.Errorf("begin account administration: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := administrationRecords{accountOperations{tx}}
	result, err := records.executeUpdate(ctx, cmd)
	if err != nil {
		return accounts.UpdateUserResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return accounts.UpdateUserResult{}, fmt.Errorf("commit account administration: %w", err)
	}
	return result, nil
}

func (repository *AdministrationRepository) CommitDeleteUser(
	ctx context.Context, cmd accounts.DeleteUserCommand,
) (bool, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin account administration: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := administrationRecords{accountOperations{tx}}
	replayed, err := records.executeDelete(ctx, cmd)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit account administration: %w", err)
	}
	return replayed, nil
}

func (records administrationRecords) executeUpdate(
	ctx context.Context, cmd accounts.UpdateUserCommand,
) (accounts.UpdateUserResult, error) {
	replay, err := records.Replay(ctx, cmd.Operation)
	if err != nil {
		return accounts.UpdateUserResult{}, fmt.Errorf("apply account administration: %w", err)
	}
	if err := accounts.CheckAccountReplay(replay, cmd.Operation); err != nil {
		return accounts.UpdateUserResult{}, fmt.Errorf("apply account administration: %w", err)
	}
	if replay.Found {
		return accounts.UpdateUserResult{Replayed: true}, nil
	}
	return records.applyUserUpdate(ctx, cmd)
}

func (records administrationRecords) applyUserUpdate(
	ctx context.Context, cmd accounts.UpdateUserCommand,
) (accounts.UpdateUserResult, error) {
	before, found, err := records.Current(ctx, cmd.TargetID, cmd.Operation.Now)
	if err != nil {
		return accounts.UpdateUserResult{}, fmt.Errorf("apply account change: %w", err)
	}
	if err := accounts.ValidateManagedUser(before, found, cmd.Version); err != nil {
		return accounts.UpdateUserResult{}, fmt.Errorf("apply account change: %w", err)
	}
	change, err := accounts.ResolveUserChange(
		before.User, cmd.Patch, cmd.Operation.PrincipalID == cmd.TargetID,
	)
	if err != nil {
		return accounts.UpdateUserResult{}, fmt.Errorf("apply account change: %w", err)
	}
	if err := records.checkLastAdmin(ctx, before.User, change); err != nil {
		return accounts.UpdateUserResult{}, err
	}
	if err := records.Update(ctx, accounts.AdministrationUpdate{
		Before: before, Change: change, Now: cmd.Operation.Now,
	}); err != nil {
		return accounts.UpdateUserResult{}, fmt.Errorf("apply account change: %w", err)
	}
	current, found, err := records.Current(ctx, cmd.TargetID, cmd.Operation.Now)
	if err != nil {
		return accounts.UpdateUserResult{}, fmt.Errorf("apply account change: %w", err)
	}
	if !found {
		return accounts.UpdateUserResult{}, accounts.ErrUserNotFound
	}
	if err := auditUserChange(ctx, records, cmd.Operation, before.User, current.User); err != nil {
		return accounts.UpdateUserResult{}, err
	}
	body, err := json.Marshal(current.User)
	if err != nil {
		return accounts.UpdateUserResult{}, fmt.Errorf("encode user update replay: %w", err)
	}
	if err := records.Remember(ctx, accountReceipt(cmd.Operation, 200, body)); err != nil {
		return accounts.UpdateUserResult{}, fmt.Errorf("apply account administration: %w", err)
	}
	return accounts.UpdateUserResult{User: current.User}, nil
}

func (records administrationRecords) checkLastAdmin(
	ctx context.Context, before accounts.AdminUser, change accounts.UserChange,
) error {
	if !accounts.RemovesEnabledAdmin(before, change.Role, change.Status) {
		return nil
	}
	exists, err := records.AnotherEnabledAdmin(ctx, before.UserID)
	if err != nil {
		return fmt.Errorf("apply account change: %w", err)
	}
	if !exists {
		return fmt.Errorf("apply account change: %w", accounts.ErrLastAdmin)
	}
	return nil
}

func accountReceipt(operation accounts.AccountOperation, status int, body []byte) accounts.AccountReceipt {
	return accounts.AccountReceipt{
		Operation: operation,
		Status:    status,
		Body:      body,
		ExpiresAt: operation.Now + int64(24*time.Hour/time.Millisecond),
	}
}

func auditUserChange(
	ctx context.Context,
	records administrationRecords,
	operation accounts.AccountOperation,
	before, after accounts.AdminUser,
) error {
	if before.Role != after.Role {
		audit, err := newAccountAudit(
			operation.PrincipalID, "USER_ROLE_CHANGED", "USER", before.UserID,
			map[string]any{"role": before.Role, "version": before.Version},
			map[string]any{"role": after.Role, "version": after.Version},
			operation.Now,
		)
		if err != nil {
			return fmt.Errorf("apply account administration: %w", err)
		}
		if err := records.Audit(ctx, audit); err != nil {
			return fmt.Errorf("apply account administration: %w", err)
		}
	}
	if before.Status != after.Status {
		action := "USER_DISABLED"
		if after.Status == "ENABLED" {
			action = "USER_ENABLED"
		}
		audit, err := newAccountAudit(
			operation.PrincipalID, action, "USER", before.UserID,
			map[string]any{"status": before.Status, "version": before.Version},
			map[string]any{"status": after.Status, "version": after.Version},
			operation.Now,
		)
		if err != nil {
			return fmt.Errorf("apply account administration: %w", err)
		}
		if err := records.Audit(ctx, audit); err != nil {
			return fmt.Errorf("apply account administration: %w", err)
		}
	}
	return nil
}

func (records administrationRecords) executeDelete(
	ctx context.Context, cmd accounts.DeleteUserCommand,
) (bool, error) {
	replay, err := records.Replay(ctx, cmd.Operation)
	if err != nil {
		return false, fmt.Errorf("apply account administration: %w", err)
	}
	if replay.Found && replay.Digest != cmd.Operation.Digest {
		return false, accounts.ErrIdempotencyReused
	}
	if replay.Found {
		return true, nil
	}
	before, found, err := records.Current(ctx, cmd.TargetID, cmd.Operation.Now)
	if err != nil {
		return false, fmt.Errorf("apply account administration: %w", err)
	}
	if err := accounts.ValidateManagedUser(before, found, cmd.Version); err != nil {
		return false, fmt.Errorf("apply account administration: %w", err)
	}
	if err := accounts.ValidateUserDeletion(before.User, cmd.ActorID, cmd.Confirmation); err != nil {
		return false, fmt.Errorf("apply account administration: %w", err)
	}
	if accounts.RemovesEnabledAdmin(before.User, before.User.Role, "DELETED") {
		exists, err := records.AnotherEnabledAdmin(ctx, cmd.TargetID)
		if err != nil {
			return false, fmt.Errorf("apply account administration: %w", err)
		}
		if !exists {
			return false, fmt.Errorf("apply account administration: %w", accounts.ErrLastAdmin)
		}
	}
	if err := records.Delete(ctx, accounts.AdministrationDeletion{
		Before: before,
		Security: accounts.UserSecurity{
			Reason: "USER_DELETED", Sessions: true,
			CreatedLinks: true, TargetLinks: true, Launches: true,
		},
		ClearTestDefault: before.User.Username == "test",
		Now:              cmd.Operation.Now,
	}); err != nil {
		return false, fmt.Errorf("apply account administration: %w", err)
	}
	audit, err := newAccountAudit(
		cmd.ActorID, "USER_DELETED", "USER", cmd.TargetID,
		map[string]any{"role": before.User.Role, "status": before.User.Status, "version": before.User.Version},
		map[string]any{"role": before.User.Role, "status": "DELETED", "version": before.User.Version + 1},
		cmd.Operation.Now,
	)
	if err != nil {
		return false, fmt.Errorf("apply account administration: %w", err)
	}
	if err := records.Audit(ctx, audit); err != nil {
		return false, fmt.Errorf("apply account administration: %w", err)
	}
	if err := records.Remember(ctx, accountReceipt(cmd.Operation, 204, nil)); err != nil {
		return false, fmt.Errorf("apply account administration: %w", err)
	}
	return false, nil
}

func (records administrationRecords) Current(
	ctx context.Context,
	id string,
	now int64,
) (accounts.ManagedUser, bool, error) {
	user, err := scanAdminUser(records.executor.QueryRowContext(ctx, adminUserProjection+` WHERE u.id=?`, now, now, id))
	if errors.Is(err, sql.ErrNoRows) {
		return accounts.ManagedUser{}, false, nil
	}
	if err != nil {
		return accounts.ManagedUser{}, false, err
	}
	result := accounts.ManagedUser{User: user}
	if err := records.executor.QueryRowContext(
		ctx,
		`SELECT profile_id FROM users WHERE id=?`,
		id,
	).Scan(
		&result.ProfileID,
	); err != nil {
		return result, false, fmt.Errorf("read managed profile: %w", err)
	}
	return result, true, nil
}

func (records administrationRecords) AnotherEnabledAdmin(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := records.executor.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM users WHERE id!=? AND role='ADMIN' AND status='ENABLED')`,
		id,
	).Scan(
		&exists,
	)
	if err != nil {
		return false, fmt.Errorf("query remaining administrator: %w", err)
	}
	return exists, nil
}

func newAccountAudit(
	actorID, action, resourceType, resourceID string,
	before, after any,
	now int64,
) (accounts.AccountAudit, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return accounts.AccountAudit{}, fmt.Errorf("create account audit identity: %w", err)
	}
	result := accounts.AccountAudit{
		ID:           id.String(),
		ActorID:      actorID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Now:          now,
	}
	if before != nil {
		value, err := repoAuditJSON(before)
		if err != nil {
			return accounts.AccountAudit{}, err
		}
		result.BeforeJSON = &value
	}
	if after != nil {
		value, err := repoAuditJSON(after)
		if err != nil {
			return accounts.AccountAudit{}, err
		}
		result.AfterJSON = &value
	}
	return result, nil
}

func repoAuditJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode account audit: %w", err)
	}
	return string(encoded), nil
}
