package accounts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
)

func normalizeUserQuery(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", ErrUserNotFound
	}
	value = norm.NFC.String(strings.TrimFunc(value, unicode.IsSpace))
	count := 0
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", ErrUserNotFound
		}
		count++
	}
	if count > 80 {
		return "", ErrUserNotFound
	}
	return value, nil
}

func scanAdminUser(scanner interface{ Scan(...any) error }) (AdminUser, error) {
	var user AdminUser
	var displayName string
	var lastLogin sql.NullInt64
	if err := scanner.Scan(
		&user.UserID, &user.Username, &displayName, &user.Role, &user.Status, &user.Version,
		&user.CreatedAtMS, &lastLogin, &user.ActiveSessionCount,
	); err != nil {
		return AdminUser{}, fmt.Errorf("scan admin user projection: %w", err)
	}
	user.DisplayName = displayName
	if user.Status == "DELETED" {
		user.DisplayName = "已删除用户"
	}
	user.LastLoginAtMS = nil
	if lastLogin.Valid {
		user.LastLoginAtMS = lastLogin.Int64
	}
	return user, nil
}

func (service *Service) GetUser(ctx context.Context, userID string) (AdminUser, error) {
	now := service.now().UTC().UnixMilli()
	user, err := scanAdminUser(service.database.QueryRowContext(ctx, `
SELECT u.id,u.username,u.display_name,u.role,u.status,u.version,u.created_at_ms,u.last_login_at_ms,
(SELECT count(*) FROM auth_sessions session
 WHERE session.user_id=u.id AND session.revoked_at_ms IS NULL
 AND session.user_session_version=u.session_version
 AND session.idle_expires_at_ms>? AND session.absolute_expires_at_ms>?
 AND u.status='ENABLED')
FROM users u WHERE u.id=?
`, now, now, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return AdminUser{}, ErrUserNotFound
	}
	if err != nil {
		return AdminUser{}, fmt.Errorf("read admin user: %w", err)
	}
	return user, nil
}

//nolint:funlen,gocyclo,gocognit // Filter validation, stable cursor predicates, and the closed sort set stay together.
func (service *Service) ListUsers(ctx context.Context, filter UserListFilter) ([]AdminUser, error) {
	queryText, err := normalizeUserQuery(filter.Query)
	if err != nil {
		return nil, err
	}
	if filter.Role != "" && filter.Role != "ADMIN" && filter.Role != "USER" {
		return nil, ErrUserNotFound
	}
	if filter.Status == "" {
		filter.Status = "NON_DELETED"
	}
	if filter.Status != "NON_DELETED" && filter.Status != "ALL" && filter.Status != "ENABLED" &&
		filter.Status != "DISABLED" && filter.Status != "DELETED" {
		return nil, ErrUserNotFound
	}
	if filter.Sort == "" {
		filter.Sort = "CREATED_DESC"
	}
	if filter.Sort != "CREATED_DESC" && filter.Sort != "USERNAME_ASC" && filter.Sort != "LAST_LOGIN_DESC" {
		return nil, ErrUserNotFound
	}
	now := service.now().UTC().UnixMilli()
	query := `
SELECT u.id,u.username,u.display_name,u.role,u.status,u.version,u.created_at_ms,u.last_login_at_ms,
(SELECT count(*) FROM auth_sessions session
 WHERE session.user_id=u.id AND session.revoked_at_ms IS NULL
 AND session.user_session_version=u.session_version
 AND session.idle_expires_at_ms>? AND session.absolute_expires_at_ms>?
 AND u.status='ENABLED')
FROM users u WHERE 1=1`
	arguments := []any{now, now}
	if queryText != "" {
		query += " AND (instr(u.username,lower(?))>0 OR instr(u.display_name,?)>0)"
		arguments = append(arguments, queryText, queryText)
	}
	if filter.Role != "" {
		query += " AND u.role=?"
		arguments = append(arguments, filter.Role)
	}
	switch filter.Status {
	case "NON_DELETED":
		query += " AND u.status!='DELETED'"
	case "ENABLED", "DISABLED", "DELETED":
		query += " AND u.status=?"
		arguments = append(arguments, filter.Status)
	}
	switch filter.Sort {
	case "CREATED_DESC":
		if filter.AfterID != "" && len(filter.AfterValues) == 1 {
			created, parseErr := strconv.ParseInt(filter.AfterValues[0], 10, 64)
			if parseErr != nil {
				return nil, ErrUserNotFound
			}
			query += " AND (u.created_at_ms<? OR (u.created_at_ms=? AND u.id<?))"
			arguments = append(arguments, created, created, filter.AfterID)
		}
		query += " ORDER BY u.created_at_ms DESC,u.id DESC"
	case "USERNAME_ASC":
		if filter.AfterID != "" && len(filter.AfterValues) == 1 {
			query += " AND (u.username>? OR (u.username=? AND u.id>?))"
			arguments = append(arguments, filter.AfterValues[0], filter.AfterValues[0], filter.AfterID)
		}
		query += " ORDER BY u.username ASC,u.id ASC"
	case "LAST_LOGIN_DESC":
		if filter.AfterID != "" && len(filter.AfterValues) == 2 {
			last, lastErr := strconv.ParseInt(filter.AfterValues[0], 10, 64)
			created, createdErr := strconv.ParseInt(filter.AfterValues[1], 10, 64)
			if lastErr != nil || createdErr != nil {
				return nil, ErrUserNotFound
			}
			if last >= 0 {
				query += ` AND (u.last_login_at_ms IS NULL OR u.last_login_at_ms<? OR
(u.last_login_at_ms=? AND u.id<?))`
				arguments = append(arguments, last, last, filter.AfterID)
			} else {
				query += ` AND u.last_login_at_ms IS NULL
AND (u.created_at_ms<? OR (u.created_at_ms=? AND u.id<?))`
				arguments = append(arguments, created, created, filter.AfterID)
			}
		}
		query += ` ORDER BY (u.last_login_at_ms IS NULL) ASC,u.last_login_at_ms DESC,
CASE WHEN u.last_login_at_ms IS NULL THEN u.created_at_ms END DESC,u.id DESC`
	}
	query += " LIMIT ?"
	arguments = append(arguments, filter.Limit)
	rows, err := service.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list admin users: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]AdminUser, 0, filter.Limit)
	for rows.Next() {
		item, scanErr := scanAdminUser(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan admin user: %w", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin users: %w", err)
	}
	return items, nil
}

func readUserState(ctx context.Context, transaction *sql.Tx, userID string) (AdminUser, string, error) {
	var user AdminUser
	var profileID string
	var lastLogin sql.NullInt64
	err := transaction.QueryRowContext(ctx, `
SELECT id,username,display_name,role,status,version,created_at_ms,last_login_at_ms,profile_id
FROM users WHERE id=?
`, userID).Scan(
		&user.UserID, &user.Username, &user.DisplayName, &user.Role, &user.Status, &user.Version,
		&user.CreatedAtMS, &lastLogin, &profileID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminUser{}, "", ErrUserNotFound
	}
	if err != nil {
		return AdminUser{}, "", fmt.Errorf("read user state: %w", err)
	}
	user.LastLoginAtMS = nil
	if lastLogin.Valid {
		user.LastLoginAtMS = lastLogin.Int64
	}
	return user, profileID, nil
}

func revokeUserSecurity(
	ctx context.Context,
	transaction *sql.Tx,
	user AdminUser,
	profileID, reason string,
	revokeCreatedLinks, revokeTargetLinks, revokeLaunches bool,
	now int64,
) error {
	if _, err := transaction.ExecContext(ctx, `
UPDATE auth_sessions SET revoked_at_ms=?,revoked_reason=?
WHERE user_id=? AND revoked_at_ms IS NULL
`, now, reason, user.UserID); err != nil {
		return fmt.Errorf("revoke user sessions: %w", err)
	}
	if revokeCreatedLinks {
		if _, err := transaction.ExecContext(ctx, `
UPDATE account_links SET revoked_at_ms=?,revoked_by_kind='SYSTEM',version=version+1
WHERE created_by_user_id=? AND consumed_at_ms IS NULL AND revoked_at_ms IS NULL AND expires_at_ms>?
`, now, user.UserID, now); err != nil {
			return fmt.Errorf("revoke user-created links: %w", err)
		}
	}
	if revokeTargetLinks {
		if _, err := transaction.ExecContext(ctx, `
UPDATE account_links SET revoked_at_ms=?,revoked_by_kind='SYSTEM',version=version+1
WHERE kind='PASSWORD_RESET' AND target_user_id=?
AND consumed_at_ms IS NULL AND revoked_at_ms IS NULL AND expires_at_ms>?
`, now, user.UserID, now); err != nil {
			return fmt.Errorf("revoke user-targeted links: %w", err)
		}
	}
	if revokeLaunches {
		if _, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions SET state='REVOKED',finished_at_ms=?,version=version+1,updated_at_ms=?
WHERE profile_id=? AND state IN ('CREATED','ACTIVE')
`, now, now, profileID); err != nil {
			return fmt.Errorf("revoke user launches: %w", err)
		}
	}
	return nil
}

//nolint:funlen,gocyclo,gocognit // User update, security revocation, audit, and idempotency commit atomically.
func (service *Service) UpdateUser(
	ctx context.Context,
	principal authn.Principal,
	targetUserID string,
	expectedVersion int64,
	patch UserPatch,
	idempotencyKey string,
) (AdminUser, bool, error) {
	digest := operationDigest("patchAdminUser", principal.UserID, map[string]any{
		"confirmAdminRole": patch.ConfirmAdminRole, "expectedVersion": expectedVersion,
		"role": patch.Role, "status": patch.Status, "targetUserId": targetUserID,
	})
	now := service.now().UTC().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return AdminUser{}, false, fmt.Errorf("begin user update: %w", err)
	}
	defer cleanup.Rollback(transaction)
	body, replayed, err := loadIdempotency(
		ctx, transaction, principal.UserID, "patchAdminUser", idempotencyKey, digest, now,
	)
	if err != nil {
		return AdminUser{}, false, err
	}
	if replayed {
		var result AdminUser
		if err := json.Unmarshal(body, &result); err != nil {
			return AdminUser{}, false, fmt.Errorf("decode user update replay: %w", err)
		}
		return result, true, nil
	}
	before, profileID, err := readUserState(ctx, transaction, targetUserID)
	if err != nil {
		return AdminUser{}, false, err
	}
	if before.Status == "DELETED" {
		return AdminUser{}, false, ErrUserDeleted
	}
	if before.Version != expectedVersion {
		return AdminUser{}, false, ErrUserVersion
	}
	role, status := before.Role, before.Status
	if patch.Role != nil {
		if *patch.Role != "ADMIN" && *patch.Role != "USER" {
			return AdminUser{}, false, ErrRoleConfirmation
		}
		role = *patch.Role
	}
	if patch.Status != nil {
		if *patch.Status != "ENABLED" && *patch.Status != "DISABLED" {
			return AdminUser{}, false, ErrUserTransition
		}
		status = *patch.Status
	}
	if patch.Role == nil && patch.Status == nil || role == before.Role && status == before.Status {
		return AdminUser{}, false, ErrUserNoChange
	}
	if (patch.Role != nil && role == "ADMIN") != patch.ConfirmAdminRole {
		return AdminUser{}, false, ErrRoleConfirmation
	}
	downgrade := before.Role == "ADMIN" && role == "USER"
	roleChange := before.Role != role
	disable := before.Status == "ENABLED" && status == "DISABLED"
	if targetUserID == principal.UserID && (downgrade || disable) {
		return AdminUser{}, false, ErrUserSelfChange
	}
	securityChange := roleChange || disable
	result, err := transaction.ExecContext(ctx, `
UPDATE users SET role=?,status=?,session_version=session_version+?,version=version+1,updated_at_ms=?,
disabled_at_ms=CASE WHEN ?='DISABLED' THEN COALESCE(disabled_at_ms,?) ELSE NULL END
WHERE id=? AND version=? AND status!='DELETED'
`, role, status, map[bool]int{true: 1, false: 0}[securityChange], now, status, now, targetUserID, expectedVersion)
	if err != nil {
		if strings.Contains(err.Error(), "last enabled admin") {
			return AdminUser{}, false, ErrLastAdmin
		}
		return AdminUser{}, false, fmt.Errorf("update user: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return AdminUser{}, false, ErrUserVersion
	}
	if securityChange {
		reason := "ROLE_CHANGED"
		if disable {
			reason = "USER_DISABLED"
		}
		if err := revokeUserSecurity(
			ctx, transaction, before, profileID, reason, downgrade || disable, disable, disable, now,
		); err != nil {
			return AdminUser{}, false, err
		}
	}
	after, _, err := readUserState(ctx, transaction, targetUserID)
	if err != nil {
		return AdminUser{}, false, err
	}
	if roleChange {
		if err := insertUserAudit(
			ctx, transaction, principal, "USER_ROLE_CHANGED", "USER", targetUserID,
			map[string]any{"role": before.Role, "version": before.Version},
			map[string]any{"role": after.Role, "version": after.Version}, now,
		); err != nil {
			return AdminUser{}, false, err
		}
	}
	if before.Status != after.Status {
		action := "USER_DISABLED"
		if after.Status == "ENABLED" {
			action = "USER_ENABLED"
		}
		if err := insertUserAudit(
			ctx, transaction, principal, action, "USER", targetUserID,
			map[string]any{"status": before.Status, "version": before.Version},
			map[string]any{"status": after.Status, "version": after.Version}, now,
		); err != nil {
			return AdminUser{}, false, err
		}
	}
	encoded, _ := json.Marshal(after)
	if err := storeIdempotency(
		ctx, transaction, principal.UserID, "patchAdminUser", idempotencyKey, digest,
		http.StatusOK, encoded, now,
	); err != nil {
		return AdminUser{}, false, err
	}
	if err := transaction.Commit(); err != nil {
		return AdminUser{}, false, fmt.Errorf("commit user update: %w", err)
	}
	return after, false, nil
}

//nolint:funlen,gocyclo // Soft delete and all credential revocations are one security transaction.
func (service *Service) DeleteUser(
	ctx context.Context,
	principal authn.Principal,
	targetUserID string,
	expectedVersion int64,
	confirmUsername, idempotencyKey string,
) (bool, error) {
	digest := operationDigest("deleteAdminUser", principal.UserID, map[string]any{
		"confirmUsername": confirmUsername, "expectedVersion": expectedVersion, "targetUserId": targetUserID,
	})
	now := service.now().UTC().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin user deletion: %w", err)
	}
	defer cleanup.Rollback(transaction)
	_, replayed, err := loadIdempotency(
		ctx, transaction, principal.UserID, "deleteAdminUser", idempotencyKey, digest, now,
	)
	if err != nil || replayed {
		return replayed, err
	}
	before, profileID, err := readUserState(ctx, transaction, targetUserID)
	if err != nil {
		return false, err
	}
	if before.Status == "DELETED" {
		return false, ErrUserDeleted
	}
	if before.Version != expectedVersion {
		return false, ErrUserVersion
	}
	if targetUserID == principal.UserID {
		return false, ErrUserSelfChange
	}
	if strings.TrimSpace(confirmUsername) != before.Username || confirmUsername != strings.TrimSpace(confirmUsername) {
		return false, ErrConfirmation
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE users SET status='DELETED',session_version=session_version+1,version=version+1,
updated_at_ms=?,disabled_at_ms=NULL,deleted_at_ms=?
WHERE id=? AND version=? AND status!='DELETED'
`, now, now, targetUserID, expectedVersion)
	if err != nil {
		if strings.Contains(err.Error(), "last enabled admin") {
			return false, ErrLastAdmin
		}
		return false, fmt.Errorf("soft-delete user: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return false, ErrUserVersion
	}
	if err := revokeUserSecurity(
		ctx, transaction, before, profileID, "USER_DELETED", true, true, true, now,
	); err != nil {
		return false, err
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM user_credentials WHERE user_id=?`, targetUserID); err != nil {
		return false, fmt.Errorf("delete user credential: %w", err)
	}
	if before.Username == "test" {
		_, _ = transaction.ExecContext(ctx, `
UPDATE instance_state SET test_default_password_active=0,version=version+1,updated_at_ms=?
WHERE id=1 AND test_default_password_active=1
`, now)
	}
	if err := insertUserAudit(
		ctx, transaction, principal, "USER_DELETED", "USER", targetUserID,
		map[string]any{"role": before.Role, "status": before.Status, "version": before.Version},
		map[string]any{"role": before.Role, "status": "DELETED", "version": before.Version + 1}, now,
	); err != nil {
		return false, err
	}
	if err := storeIdempotency(
		ctx, transaction, principal.UserID, "deleteAdminUser", idempotencyKey, digest,
		http.StatusNoContent, nil, now,
	); err != nil {
		return false, err
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit user deletion: %w", err)
	}
	return false, nil
}
