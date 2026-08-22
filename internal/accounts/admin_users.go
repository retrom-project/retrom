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

func (service *Service) ListUsers(ctx context.Context, filter UserListFilter) ([]AdminUser, error) {
	filter, queryText, err := validateUserListFilter(filter)
	if err != nil {
		return nil, err
	}
	now := service.now().UTC().UnixMilli()
	builder := userListQuery{query: `
SELECT u.id,u.username,u.display_name,u.role,u.status,u.version,u.created_at_ms,u.last_login_at_ms,
(SELECT count(*) FROM auth_sessions session
 WHERE session.user_id=u.id AND session.revoked_at_ms IS NULL
 AND session.user_session_version=u.session_version
 AND session.idle_expires_at_ms>? AND session.absolute_expires_at_ms>?
 AND u.status='ENABLED')
FROM users u WHERE 1=1`, arguments: []any{now, now}}
	if queryText != "" {
		builder.add(" AND (instr(u.username,lower(?))>0 OR instr(u.display_name,?)>0)", queryText, queryText)
	}
	if filter.Role != "" {
		builder.add(" AND u.role=?", filter.Role)
	}
	builder.addStatus(filter.Status)
	if err := builder.addSort(filter); err != nil {
		return nil, err
	}
	builder.add(" LIMIT ?", filter.Limit)
	rows, err := service.database.QueryContext(ctx, builder.query, builder.arguments...)
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

type userListQuery struct {
	query     string
	arguments []any
}

func (builder *userListQuery) add(fragment string, arguments ...any) {
	builder.query += fragment
	builder.arguments = append(builder.arguments, arguments...)
}

func validateUserListFilter(filter UserListFilter) (UserListFilter, string, error) {
	queryText, err := normalizeUserQuery(filter.Query)
	if err != nil || filter.Role != "" && filter.Role != "ADMIN" && filter.Role != "USER" {
		return filter, "", ErrUserNotFound
	}
	if filter.Status == "" {
		filter.Status = "NON_DELETED"
	}
	validStatus := map[string]bool{
		"NON_DELETED": true, "ALL": true, "ENABLED": true, "DISABLED": true, "DELETED": true,
	}
	if !validStatus[filter.Status] {
		return filter, "", ErrUserNotFound
	}
	if filter.Sort == "" {
		filter.Sort = "CREATED_DESC"
	}
	validSort := map[string]bool{"CREATED_DESC": true, "USERNAME_ASC": true, "LAST_LOGIN_DESC": true}
	if !validSort[filter.Sort] {
		return filter, "", ErrUserNotFound
	}
	return filter, queryText, nil
}

func (builder *userListQuery) addStatus(status string) {
	if status == "NON_DELETED" {
		builder.add(" AND u.status!='DELETED'")
	}
	if status == "ENABLED" || status == "DISABLED" || status == "DELETED" {
		builder.add(" AND u.status=?", status)
	}
}

func (builder *userListQuery) addSort(filter UserListFilter) error {
	switch filter.Sort {
	case "CREATED_DESC":
		return builder.addCreatedSort(filter)
	case "USERNAME_ASC":
		builder.addUsernameSort(filter)
	case "LAST_LOGIN_DESC":
		return builder.addLastLoginSort(filter)
	}
	return nil
}

func (builder *userListQuery) addCreatedSort(filter UserListFilter) error {
	if filter.AfterID != "" && len(filter.AfterValues) == 1 {
		created, err := strconv.ParseInt(filter.AfterValues[0], 10, 64)
		if err != nil {
			return ErrUserNotFound
		}
		builder.add(" AND (u.created_at_ms<? OR (u.created_at_ms=? AND u.id<?))", created, created, filter.AfterID)
	}
	builder.add(" ORDER BY u.created_at_ms DESC,u.id DESC")
	return nil
}

func (builder *userListQuery) addUsernameSort(filter UserListFilter) {
	if filter.AfterID != "" && len(filter.AfterValues) == 1 {
		builder.add(
			" AND (u.username>? OR (u.username=? AND u.id>?))",
			filter.AfterValues[0], filter.AfterValues[0], filter.AfterID,
		)
	}
	builder.add(" ORDER BY u.username ASC,u.id ASC")
}

func (builder *userListQuery) addLastLoginSort(filter UserListFilter) error {
	if filter.AfterID != "" && len(filter.AfterValues) == 2 {
		last, lastErr := strconv.ParseInt(filter.AfterValues[0], 10, 64)
		created, createdErr := strconv.ParseInt(filter.AfterValues[1], 10, 64)
		if lastErr != nil || createdErr != nil {
			return ErrUserNotFound
		}
		if last >= 0 {
			builder.add(
				" AND (u.last_login_at_ms IS NULL OR u.last_login_at_ms<? OR "+
					"(u.last_login_at_ms=? AND u.id<?))",
				last, last, filter.AfterID,
			)
		} else {
			builder.add(
				" AND u.last_login_at_ms IS NULL "+
					"AND (u.created_at_ms<? OR (u.created_at_ms=? AND u.id<?))",
				created, created, filter.AfterID,
			)
		}
	}
	builder.add(" ORDER BY (u.last_login_at_ms IS NULL) ASC,u.last_login_at_ms DESC," +
		"CASE WHEN u.last_login_at_ms IS NULL THEN u.created_at_ms END DESC,u.id DESC")
	return nil
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

func requireAnotherEnabledAdmin(
	ctx context.Context,
	transaction *sql.Tx,
	targetUserID string,
) error {
	var exists int
	if err := transaction.QueryRowContext(ctx, `
SELECT EXISTS(
  SELECT 1 FROM users
  WHERE id!=? AND role='ADMIN' AND status='ENABLED'
)
`, targetUserID).Scan(&exists); err != nil {
		return fmt.Errorf("check remaining enabled administrator: %w", err)
	}
	if exists == 0 {
		return ErrLastAdmin
	}
	return nil
}

// User update, security revocation, audit, and idempotency commit atomically.
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
	before, profileID, err := loadUserForUpdate(ctx, transaction, targetUserID, expectedVersion)
	if err != nil {
		return AdminUser{}, false, err
	}
	change, err := resolveUserChange(before, patch, targetUserID == principal.UserID)
	if err != nil {
		return AdminUser{}, false, err
	}
	if before.Role == "ADMIN" && before.Status == "ENABLED" &&
		(change.role != "ADMIN" || change.status != "ENABLED") {
		if err := requireAnotherEnabledAdmin(ctx, transaction, targetUserID); err != nil {
			return AdminUser{}, false, err
		}
	}
	if err := applyUserChange(ctx, transaction, before, profileID, change, expectedVersion, now); err != nil {
		return AdminUser{}, false, err
	}
	after, err := finishUserUpdate(ctx, transaction, principal, before, targetUserID, idempotencyKey, digest, now)
	return after, false, err
}

type userChange struct {
	role, status               string
	roleChanged, disabled      bool
	downgraded, securityChange bool
}

func resolveUserChange(before AdminUser, patch UserPatch, self bool) (userChange, error) {
	role, err := resolvedRole(before.Role, patch.Role)
	if err != nil {
		return userChange{}, err
	}
	status, err := resolvedStatus(before.Status, patch.Status)
	if err != nil {
		return userChange{}, err
	}
	change := userChange{role: role, status: status}
	if patch.Role == nil && patch.Status == nil || change.role == before.Role && change.status == before.Status {
		return userChange{}, ErrUserNoChange
	}
	if (patch.Role != nil && change.role == "ADMIN") != patch.ConfirmAdminRole {
		return userChange{}, ErrRoleConfirmation
	}
	change.roleChanged = before.Role != change.role
	change.downgraded = before.Role == "ADMIN" && change.role == "USER"
	change.disabled = before.Status == "ENABLED" && change.status == "DISABLED"
	change.securityChange = change.roleChanged || change.disabled
	if self && (change.downgraded || change.disabled) {
		return userChange{}, ErrUserSelfChange
	}
	return change, nil
}

func resolvedRole(current string, requested *string) (string, error) {
	if requested == nil {
		return current, nil
	}
	if *requested != "ADMIN" && *requested != "USER" {
		return "", ErrRoleConfirmation
	}
	return *requested, nil
}

func resolvedStatus(current string, requested *string) (string, error) {
	if requested == nil {
		return current, nil
	}
	if *requested != "ENABLED" && *requested != "DISABLED" {
		return "", ErrUserTransition
	}
	return *requested, nil
}

func loadUserForUpdate(
	ctx context.Context,
	transaction *sql.Tx,
	targetUserID string,
	expectedVersion int64,
) (AdminUser, string, error) {
	before, profileID, err := readUserState(ctx, transaction, targetUserID)
	if err != nil {
		return AdminUser{}, "", err
	}
	if before.Status == "DELETED" {
		return AdminUser{}, "", ErrUserDeleted
	}
	if before.Version != expectedVersion {
		return AdminUser{}, "", ErrUserVersion
	}
	return before, profileID, nil
}

func finishUserUpdate(
	ctx context.Context,
	transaction *sql.Tx,
	principal authn.Principal,
	before AdminUser,
	targetUserID, idempotencyKey string,
	digest string,
	now int64,
) (AdminUser, error) {
	after, _, err := readUserState(ctx, transaction, targetUserID)
	if err != nil {
		return AdminUser{}, err
	}
	if err := auditUserChange(ctx, transaction, principal, before, after, now); err != nil {
		return AdminUser{}, err
	}
	encoded, _ := json.Marshal(after)
	if err := storeIdempotency(
		ctx, transaction, principal.UserID, "patchAdminUser", idempotencyKey, digest,
		http.StatusOK, encoded, now,
	); err != nil {
		return AdminUser{}, err
	}
	if err := transaction.Commit(); err != nil {
		return AdminUser{}, fmt.Errorf("commit user update: %w", err)
	}
	return after, nil
}

func applyUserChange(
	ctx context.Context,
	transaction *sql.Tx,
	before AdminUser,
	profileID string,
	change userChange,
	expectedVersion, now int64,
) error {
	result, err := transaction.ExecContext(ctx, `
UPDATE users SET role=?,status=?,session_version=session_version+?,version=version+1,updated_at_ms=?,
disabled_at_ms=CASE WHEN ?='DISABLED' THEN COALESCE(disabled_at_ms,?) ELSE NULL END
WHERE id=? AND version=? AND status!='DELETED'
`,
		change.role, change.status, boolToInt(change.securityChange), now,
		change.status, now, before.UserID, expectedVersion,
	)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrUserVersion
	}
	if change.securityChange {
		reason := "ROLE_CHANGED"
		if change.disabled {
			reason = "USER_DISABLED"
		}
		if err := revokeUserSecurity(
			ctx, transaction, before, profileID, reason,
			change.downgraded || change.disabled, change.disabled, change.disabled, now,
		); err != nil {
			return err
		}
	}
	return nil
}

func auditUserChange(
	ctx context.Context,
	transaction *sql.Tx,
	principal authn.Principal,
	before, after AdminUser,
	now int64,
) error {
	if before.Role != after.Role {
		if err := insertUserAudit(
			ctx, transaction, principal, "USER_ROLE_CHANGED", "USER", before.UserID,
			map[string]any{"role": before.Role, "version": before.Version},
			map[string]any{"role": after.Role, "version": after.Version}, now,
		); err != nil {
			return err
		}
	}
	if before.Status != after.Status {
		action := "USER_DISABLED"
		if after.Status == "ENABLED" {
			action = "USER_ENABLED"
		}
		if err := insertUserAudit(
			ctx, transaction, principal, action, "USER", before.UserID,
			map[string]any{"status": before.Status, "version": before.Version},
			map[string]any{"status": after.Status, "version": after.Version}, now,
		); err != nil {
			return err
		}
	}
	return nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// Soft delete and all credential revocations are one security transaction.
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
	if err := validateUserDeletion(
		ctx, transaction, principal.UserID, before, expectedVersion, confirmUsername,
	); err != nil {
		return false, err
	}
	if err := persistUserDeletion(ctx, transaction, before, profileID, expectedVersion, now); err != nil {
		return false, err
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

func validateUserDeletion(
	ctx context.Context,
	transaction *sql.Tx,
	principalUserID string,
	before AdminUser,
	expectedVersion int64,
	confirmUsername string,
) error {
	if before.Status == "DELETED" {
		return ErrUserDeleted
	}
	if before.Version != expectedVersion {
		return ErrUserVersion
	}
	if before.UserID == principalUserID {
		return ErrUserSelfChange
	}
	trimmedConfirmation := strings.TrimSpace(confirmUsername)
	if trimmedConfirmation != before.Username || confirmUsername != trimmedConfirmation {
		return ErrConfirmation
	}
	if before.Role == "ADMIN" && before.Status == "ENABLED" {
		return requireAnotherEnabledAdmin(ctx, transaction, before.UserID)
	}
	return nil
}

func persistUserDeletion(
	ctx context.Context,
	transaction *sql.Tx,
	before AdminUser,
	profileID string,
	expectedVersion, now int64,
) error {
	result, err := transaction.ExecContext(ctx, `
UPDATE users SET status='DELETED',session_version=session_version+1,version=version+1,
updated_at_ms=?,disabled_at_ms=NULL,deleted_at_ms=?
WHERE id=? AND version=? AND status!='DELETED'
`, now, now, before.UserID, expectedVersion)
	if err != nil {
		return fmt.Errorf("soft-delete user: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrUserVersion
	}
	if err := revokeUserSecurity(
		ctx, transaction, before, profileID, "USER_DELETED", true, true, true, now,
	); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM user_credentials WHERE user_id=?`, before.UserID); err != nil {
		return fmt.Errorf("delete user credential: %w", err)
	}
	if before.Username == "test" {
		_, _ = transaction.ExecContext(ctx, `
UPDATE instance_state SET test_default_password_active=0,version=version+1,updated_at_ms=?
WHERE id=1 AND test_default_password_active=1
`, now)
	}
	return nil
}
