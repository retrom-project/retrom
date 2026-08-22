package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	retromruntime "retrom/internal/runtime"
)

var ErrOfflineAdmin = errors.New("OFFLINE_ADMIN_INVALID")

func ReadSetupCode(
	ctx context.Context,
	database *sql.DB,
	credentials *retromruntime.Credentials,
) (string, error) {
	var state string
	var users, profiles int
	err := database.QueryRowContext(ctx, `
SELECT state,(SELECT count(*) FROM users),(SELECT count(*) FROM profiles)
FROM instance_state WHERE id=1
`).Scan(&state, &users, &profiles)
	if err != nil {
		return "", fmt.Errorf("read setup-code state: %w", err)
	}
	if state != "PENDING" || users != 0 || profiles != 0 {
		return "", ErrInitializationDone
	}
	return credentials.SetupCode(), nil
}

// Credential reset, revocations, re-enable, and audit are one transaction.
func (service *Service) OfflineAdminReset(
	ctx context.Context,
	usernameInput, passwordInput, confirmationInput string,
) error {
	input, err := service.prepareOfflineAdminReset(ctx, usernameInput, passwordInput, confirmationInput)
	if err != nil {
		return err
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin offline admin recovery: %w", err)
	}
	defer cleanup.Rollback(transaction)
	return applyOfflineAdminReset(ctx, transaction, input)
}

type offlineAdminReset struct {
	userID, username, status, encodedPassword string
	version                                   int64
	now                                       int64
}

func (service *Service) prepareOfflineAdminReset(
	ctx context.Context,
	usernameInput, passwordInput, confirmationInput string,
) (offlineAdminReset, error) {
	username, err := authn.NormalizeUsername(usernameInput)
	if err != nil {
		return offlineAdminReset{}, ErrOfflineAdmin
	}
	var userID, displayName, role, status string
	var version int64
	err = service.database.QueryRowContext(ctx, `
SELECT id,display_name,role,status,version FROM users WHERE username=?
`, username).Scan(&userID, &displayName, &role, &status, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return offlineAdminReset{}, ErrOfflineAdmin
	}
	if err != nil {
		return offlineAdminReset{}, fmt.Errorf("read offline recovery target: %w", err)
	}
	if role != "ADMIN" || status == "DELETED" {
		return offlineAdminReset{}, ErrOfflineAdmin
	}
	password, err := authn.ValidatePassword(
		passwordInput, confirmationInput, username, displayName, service.blocklist,
	)
	if err != nil {
		return offlineAdminReset{}, fmt.Errorf("validate offline recovery password: %w", err)
	}
	encoded, err := service.hasher.Hash(ctx, password)
	if err != nil {
		return offlineAdminReset{}, fmt.Errorf("hash offline recovery password: %w", err)
	}
	return offlineAdminReset{
		userID: userID, username: username, status: status, encodedPassword: encoded,
		version: version, now: service.now().UTC().UnixMilli(),
	}, nil
}

func applyOfflineAdminReset(ctx context.Context, transaction *sql.Tx, input offlineAdminReset) error {
	var currentRole, currentStatus string
	var currentVersion int64
	if err := transaction.QueryRowContext(ctx, `
SELECT role,status,version FROM users WHERE id=?
`, input.userID).Scan(&currentRole, &currentStatus, &currentVersion); err != nil {
		return fmt.Errorf("recheck offline recovery target: %w", err)
	}
	if currentRole != "ADMIN" || currentStatus == "DELETED" || currentVersion != input.version {
		return ErrOfflineAdmin
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE users SET status='ENABLED',session_version=session_version+1,version=version+1,
updated_at_ms=?,disabled_at_ms=NULL WHERE id=? AND version=? AND role='ADMIN' AND status!='DELETED'
`, input.now, input.userID, input.version)
	if err != nil {
		return fmt.Errorf("enable offline recovery admin: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrOfflineAdmin
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE user_credentials SET password_hash=?,password_scheme='ARGON2ID_V1',password_changed_at_ms=?
WHERE user_id=?
`, input.encodedPassword, input.now, input.userID); err != nil {
		return fmt.Errorf("replace offline recovery credential: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE auth_sessions SET revoked_at_ms=?,revoked_reason='OFFLINE_RECOVERY'
WHERE user_id=? AND revoked_at_ms IS NULL
`, input.now, input.userID); err != nil {
		return fmt.Errorf("revoke offline recovery sessions: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE account_links SET revoked_at_ms=?,revoked_by_kind='SYSTEM',version=version+1
WHERE kind='PASSWORD_RESET' AND target_user_id=?
AND consumed_at_ms IS NULL AND revoked_at_ms IS NULL AND expires_at_ms>?
`, input.now, input.userID, input.now); err != nil {
		return fmt.Errorf("revoke offline recovery links: %w", err)
	}
	if input.username == "test" {
		if _, err := transaction.ExecContext(ctx, `
UPDATE instance_state SET test_default_password_active=0,version=version+1,updated_at_ms=?
WHERE id=1 AND test_default_password_active=1
`, input.now); err != nil {
			return fmt.Errorf("clear offline recovery test credential flag: %w", err)
		}
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO audit_events(
id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms)
VALUES(?,'SYSTEM',NULL,'offline-recovery','ADMIN_OFFLINE_RECOVERED','USER',?,
json_object('status',?,'version',?),json_object('status','ENABLED','version',?),'{}',NULL,?)
`, newID(), input.userID, input.status, input.version, input.version+1, input.now); err != nil {
		return fmt.Errorf("audit offline admin recovery: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit offline admin recovery: %w", err)
	}
	return nil
}
