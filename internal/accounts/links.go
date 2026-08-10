package accounts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
)

type AcceptInvitationRequest struct {
	Token, Username, DisplayName, Password, PasswordConfirmation string
}

type CompletePasswordResetRequest struct {
	Token, Password, PasswordConfirmation string
}

type PasswordResetResult struct {
	Session *Session
	Status  string
}

func accountLinkState(consumedAt, revokedAt sql.NullInt64, expiresAt, now int64) string {
	switch {
	case consumedAt.Valid:
		return "CONSUMED"
	case revokedAt.Valid:
		return "REVOKED"
	case now >= expiresAt:
		return "EXPIRED"
	default:
		return "ACTIVE"
	}
}

func (service *Service) linkToken(kind, id string) string {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return ""
	}
	return service.credentials.AccountLinkToken(kind, parsed)
}

func (service *Service) CreateInvitation(
	ctx context.Context,
	principal authn.Principal,
	role string,
	confirmAdminRole bool,
	idempotencyKey string,
) (AccountLink, bool, error) {
	if role != "USER" && role != "ADMIN" || role == "ADMIN" != confirmAdminRole {
		return AccountLink{}, false, ErrRoleConfirmation
	}
	digest := operationDigest("postAdminInvitation", principal.UserID, map[string]any{
		"confirmAdminRole": confirmAdminRole,
		"role":             role,
	})
	now := service.now().UTC().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return AccountLink{}, false, fmt.Errorf("begin invitation creation: %w", err)
	}
	defer cleanup.Rollback(transaction)
	body, replayed, err := loadIdempotency(
		ctx, transaction, principal.UserID, "postAdminInvitation", idempotencyKey, digest, now,
	)
	if err != nil {
		return AccountLink{}, false, err
	}
	if replayed {
		var result AccountLink
		if err := json.Unmarshal(body, &result); err != nil {
			return AccountLink{}, false, fmt.Errorf("decode invitation replay: %w", err)
		}
		result.CapabilityToken = service.linkToken("INVITATION", result.AccountLinkID)
		return result, true, nil
	}
	linkID := newID()
	expires := now + int64(time.Hour/time.Millisecond)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO account_links(
id,kind,invited_role,target_user_id,created_by_user_id,created_at_ms,expires_at_ms,version)
VALUES(?,'INVITATION',?,NULL,?,?,?,1)
`, linkID, role, principal.UserID, now, expires); err != nil {
		return AccountLink{}, false, fmt.Errorf("create invitation: %w", err)
	}
	result := AccountLink{
		AccountLinkID: linkID, Kind: "INVITATION", Role: role, TargetUserID: nil,
		CreatedBy: map[string]any{"userId": principal.UserID, "username": principal.Username},
		State:     "ACTIVE", Version: 1, CreatedAtMS: now, ExpiresAtMS: expires,
		ConsumedAtMS: nil, RevokedAtMS: nil,
	}
	if err := insertUserAudit(
		ctx, transaction, principal, "INVITATION_CREATED", "ACCOUNT_LINK", linkID,
		nil, map[string]any{"kind": "INVITATION", "role": role, "expiresAtMs": expires}, now,
	); err != nil {
		return AccountLink{}, false, err
	}
	encoded, _ := json.Marshal(result)
	if err := storeIdempotency(
		ctx, transaction, principal.UserID, "postAdminInvitation", idempotencyKey, digest,
		http.StatusCreated, encoded, now,
	); err != nil {
		return AccountLink{}, false, err
	}
	if err := transaction.Commit(); err != nil {
		return AccountLink{}, false, fmt.Errorf("commit invitation creation: %w", err)
	}
	result.CapabilityToken = service.linkToken("INVITATION", linkID)
	return result, false, nil
}

func (service *Service) InspectAccountLink(
	ctx context.Context,
	expectedKind, token string,
) (LinkInspection, error) {
	if expectedKind != "INVITATION" && expectedKind != "PASSWORD_RESET" {
		return LinkInspection{}, ErrAccountLinkUnavailable
	}
	linkID, valid := service.credentials.ParseAccountLinkToken(expectedKind, token)
	if !valid {
		return LinkInspection{}, ErrAccountLinkUnavailable
	}
	var kind string
	var role, username sql.NullString
	var expiresAt int64
	var consumedAt, revokedAt sql.NullInt64
	err := service.database.QueryRowContext(ctx, `
SELECT link.kind,link.invited_role,user.username,link.expires_at_ms,link.consumed_at_ms,link.revoked_at_ms
FROM account_links link
LEFT JOIN users user ON user.id=link.target_user_id
WHERE link.id=? AND link.kind=?
`, linkID.String(), expectedKind).Scan(&kind, &role, &username, &expiresAt, &consumedAt, &revokedAt)
	if err != nil || accountLinkState(consumedAt, revokedAt, expiresAt, service.now().UTC().UnixMilli()) != "ACTIVE" {
		return LinkInspection{}, ErrAccountLinkUnavailable
	}
	result := LinkInspection{Kind: kind, Role: nil, Username: nil, ExpiresAtMS: expiresAt}
	if role.Valid {
		result.Role = role.String
	}
	if username.Valid {
		result.Username = username.String
	}
	return result, nil
}

//nolint:funlen,gocyclo // Invitation consumption must create identity, credential, session, and audit atomically.
func (service *Service) AcceptInvitation(
	ctx context.Context,
	request AcceptInvitationRequest,
) (Session, error) {
	linkID, valid := service.credentials.ParseAccountLinkToken("INVITATION", request.Token)
	if !valid {
		return Session{}, ErrAccountLinkUnavailable
	}
	username, err := authn.NormalizeUsername(request.Username)
	if err != nil {
		return Session{}, fmt.Errorf("normalize invited username: %w", err)
	}
	displayName, err := authn.NormalizeDisplayName(request.DisplayName)
	if err != nil {
		return Session{}, fmt.Errorf("normalize invited display name: %w", err)
	}
	password, err := authn.ValidatePassword(
		request.Password, request.PasswordConfirmation, username, displayName, service.blocklist,
	)
	if err != nil {
		return Session{}, fmt.Errorf("validate invited password: %w", err)
	}
	encoded, err := service.hasher.Hash(ctx, password)
	if err != nil {
		return Session{}, fmt.Errorf("hash invited password: %w", err)
	}
	prepared, err := service.prepareSession()
	if err != nil {
		return Session{}, err
	}
	userID, profileID := newID(), newID()
	now := service.now().UTC().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, fmt.Errorf("begin invitation acceptance: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var role string
	var expiresAt int64
	var consumedAt, revokedAt sql.NullInt64
	if err := transaction.QueryRowContext(ctx, `
SELECT invited_role,expires_at_ms,consumed_at_ms,revoked_at_ms
FROM account_links WHERE id=? AND kind='INVITATION'
`, linkID.String()).Scan(&role, &expiresAt, &consumedAt, &revokedAt); err != nil ||
		accountLinkState(consumedAt, revokedAt, expiresAt, now) != "ACTIVE" {
		return Session{}, ErrAccountLinkUnavailable
	}
	var usernameExists int
	if err := transaction.QueryRowContext(ctx, `
SELECT EXISTS(SELECT 1 FROM users WHERE username=?)
`, username).Scan(&usernameExists); err != nil {
		return Session{}, fmt.Errorf("check invited username: %w", err)
	}
	if usernameExists != 0 {
		return Session{}, ErrUsernameUnavailable
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,?,?)
`, profileID, displayName, now); err != nil {
		return Session{}, fmt.Errorf("create invited profile: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,'ENABLED',?,?)
ON CONFLICT(username) DO NOTHING
`, userID, profileID, username, displayName, role, now, now)
	if err != nil {
		return Session{}, fmt.Errorf("create invited user: %w", err)
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil {
		return Session{}, fmt.Errorf("read invited user insert result: %w", rowsErr)
	} else if changed != 1 {
		return Session{}, ErrUsernameUnavailable
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO user_credentials(user_id,password_hash,password_scheme,password_changed_at_ms,created_at_ms)
VALUES(?,?,'ARGON2ID_V1',?,?)
`, userID, encoded, now, now); err != nil {
		return Session{}, fmt.Errorf("create invited credential: %w", err)
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE account_links SET consumed_at_ms=?,consumed_by_user_id=?,version=version+1
WHERE id=? AND consumed_at_ms IS NULL AND revoked_at_ms IS NULL AND expires_at_ms>?
`, now, userID, linkID.String(), now)
	if err != nil {
		return Session{}, fmt.Errorf("consume invitation: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return Session{}, ErrAccountLinkUnavailable
	}
	if err := insertPreparedSession(ctx, transaction, prepared, userID, 1, now); err != nil {
		return Session{}, err
	}
	principal := authn.Principal{UserID: userID, Username: username}
	if err := insertUserAudit(
		ctx, transaction, principal, "INVITATION_ACCEPTED", "ACCOUNT_LINK", linkID.String(),
		nil, map[string]any{"role": role, "status": "CONSUMED", "userId": userID}, now,
	); err != nil {
		return Session{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Session{}, fmt.Errorf("commit invitation acceptance: %w", err)
	}
	return prepared.view(
		User{UserID: userID, Username: username, DisplayName: displayName, Role: role},
		profileID, 1, now,
	), nil
}

//nolint:funlen,gocyclo // Closed transaction preserves exact version and idempotency semantics.
func (service *Service) CreatePasswordReset(
	ctx context.Context,
	principal authn.Principal,
	targetUserID string,
	expectedVersion int64,
	idempotencyKey string,
) (AccountLink, bool, error) {
	digest := operationDigest("postAdminUserPasswordResetLink", principal.UserID, map[string]any{
		"expectedVersion": expectedVersion,
		"targetUserId":    targetUserID,
	})
	now := service.now().UTC().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return AccountLink{}, false, fmt.Errorf("begin password reset creation: %w", err)
	}
	defer cleanup.Rollback(transaction)
	body, replayed, err := loadIdempotency(
		ctx, transaction, principal.UserID, "postAdminUserPasswordResetLink", idempotencyKey, digest, now,
	)
	if err != nil {
		return AccountLink{}, false, err
	}
	if replayed {
		var result AccountLink
		if err := json.Unmarshal(body, &result); err != nil {
			return AccountLink{}, false, fmt.Errorf("decode password-reset replay: %w", err)
		}
		result.CapabilityToken = service.linkToken("PASSWORD_RESET", result.AccountLinkID)
		return result, true, nil
	}
	var status string
	var currentVersion int64
	if err := transaction.QueryRowContext(ctx, `
SELECT status,version FROM users WHERE id=?
`, targetUserID).Scan(&status, &currentVersion); errors.Is(err, sql.ErrNoRows) {
		return AccountLink{}, false, ErrUserNotFound
	} else if err != nil {
		return AccountLink{}, false, fmt.Errorf("read password-reset target: %w", err)
	}
	if status == "DELETED" {
		return AccountLink{}, false, ErrUserDeleted
	}
	if currentVersion != expectedVersion {
		return AccountLink{}, false, ErrUserVersion
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE users SET version=version+1,updated_at_ms=? WHERE id=? AND version=? AND status!='DELETED'
`, now, targetUserID, expectedVersion)
	if err != nil {
		return AccountLink{}, false, fmt.Errorf("version password-reset target: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return AccountLink{}, false, ErrUserVersion
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE account_links
SET revoked_at_ms=?,revoked_by_kind='SYSTEM',version=version+1
WHERE kind='PASSWORD_RESET' AND target_user_id=?
AND consumed_at_ms IS NULL AND revoked_at_ms IS NULL AND expires_at_ms>?
`, now, targetUserID, now); err != nil {
		return AccountLink{}, false, fmt.Errorf("revoke prior password-reset links: %w", err)
	}
	linkID := newID()
	expires := now + int64(time.Hour/time.Millisecond)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO account_links(
id,kind,invited_role,target_user_id,created_by_user_id,created_at_ms,expires_at_ms,version)
VALUES(?,'PASSWORD_RESET',NULL,?,?,?,?,1)
`, linkID, targetUserID, principal.UserID, now, expires); err != nil {
		return AccountLink{}, false, fmt.Errorf("create password-reset link: %w", err)
	}
	resultLink := AccountLink{
		AccountLinkID: linkID, Kind: "PASSWORD_RESET", Role: nil, TargetUserID: targetUserID,
		CreatedBy: map[string]any{"userId": principal.UserID, "username": principal.Username},
		State:     "ACTIVE", Version: 1, CreatedAtMS: now, ExpiresAtMS: expires,
		ConsumedAtMS: nil, RevokedAtMS: nil, TargetVersion: expectedVersion + 1,
	}
	if err := insertUserAudit(
		ctx, transaction, principal, "PASSWORD_RESET_CREATED", "ACCOUNT_LINK", linkID,
		nil, map[string]any{"targetUserId": targetUserID, "expiresAtMs": expires}, now,
	); err != nil {
		return AccountLink{}, false, err
	}
	encoded, _ := json.Marshal(resultLink)
	if err := storeIdempotency(
		ctx, transaction, principal.UserID, "postAdminUserPasswordResetLink", idempotencyKey,
		digest, http.StatusCreated, encoded, now,
	); err != nil {
		return AccountLink{}, false, err
	}
	if err := transaction.Commit(); err != nil {
		return AccountLink{}, false, fmt.Errorf("commit password-reset creation: %w", err)
	}
	resultLink.CapabilityToken = service.linkToken("PASSWORD_RESET", linkID)
	return resultLink, false, nil
}

//nolint:funlen,gocyclo // Capability consumption, password rotation, revocation, and optional session are atomic.
func (service *Service) CompletePasswordReset(
	ctx context.Context,
	request CompletePasswordResetRequest,
) (PasswordResetResult, error) {
	linkID, valid := service.credentials.ParseAccountLinkToken("PASSWORD_RESET", request.Token)
	if !valid {
		return PasswordResetResult{}, ErrAccountLinkUnavailable
	}
	var targetUserID, username, displayName, status string
	var expiresAt int64
	var consumedAt, revokedAt sql.NullInt64
	if err := service.database.QueryRowContext(ctx, `
SELECT link.target_user_id,user.username,user.display_name,user.status,
link.expires_at_ms,link.consumed_at_ms,link.revoked_at_ms
FROM account_links link JOIN users user ON user.id=link.target_user_id
WHERE link.id=? AND link.kind='PASSWORD_RESET'
`, linkID.String()).Scan(
		&targetUserID, &username, &displayName, &status, &expiresAt, &consumedAt, &revokedAt,
	); err != nil || status == "DELETED" ||
		accountLinkState(consumedAt, revokedAt, expiresAt, service.now().UTC().UnixMilli()) != "ACTIVE" {
		return PasswordResetResult{}, ErrAccountLinkUnavailable
	}
	password, err := authn.ValidatePassword(
		request.Password, request.PasswordConfirmation, username, displayName, service.blocklist,
	)
	if err != nil {
		return PasswordResetResult{}, fmt.Errorf("validate reset password: %w", err)
	}
	encoded, err := service.hasher.Hash(ctx, password)
	if err != nil {
		return PasswordResetResult{}, fmt.Errorf("hash reset password: %w", err)
	}
	prepared := preparedSession{}
	if status == "ENABLED" {
		prepared, err = service.prepareSession()
		if err != nil {
			return PasswordResetResult{}, err
		}
	}
	now := service.now().UTC().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return PasswordResetResult{}, fmt.Errorf("begin password reset: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var role, profileID, currentStatus string
	var sessionVersion int64
	if err := transaction.QueryRowContext(ctx, `
UPDATE users SET session_version=session_version+1,version=version+1,updated_at_ms=?
WHERE id=? AND status!='DELETED'
RETURNING role,profile_id,status,session_version
`, now, targetUserID).Scan(&role, &profileID, &currentStatus, &sessionVersion); err != nil {
		return PasswordResetResult{}, ErrAccountLinkUnavailable
	}
	consume, err := transaction.ExecContext(ctx, `
UPDATE account_links SET consumed_at_ms=?,consumed_by_user_id=?,version=version+1
WHERE id=? AND kind='PASSWORD_RESET'
AND consumed_at_ms IS NULL AND revoked_at_ms IS NULL AND expires_at_ms>?
`, now, targetUserID, linkID.String(), now)
	if err != nil {
		return PasswordResetResult{}, fmt.Errorf("consume password-reset link: %w", err)
	}
	if changed, _ := consume.RowsAffected(); changed != 1 {
		return PasswordResetResult{}, ErrAccountLinkUnavailable
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE user_credentials SET password_hash=?,password_changed_at_ms=? WHERE user_id=?
`, encoded, now, targetUserID); err != nil {
		return PasswordResetResult{}, fmt.Errorf("replace reset credential: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE auth_sessions SET revoked_at_ms=?,revoked_reason='PASSWORD_RESET'
WHERE user_id=? AND revoked_at_ms IS NULL
`, now, targetUserID); err != nil {
		return PasswordResetResult{}, fmt.Errorf("revoke reset sessions: %w", err)
	}
	if username == "test" {
		_, _ = transaction.ExecContext(ctx, `
UPDATE instance_state SET test_default_password_active=0,version=version+1,updated_at_ms=?
WHERE id=1 AND test_default_password_active=1
`, now)
	}
	principal := authn.Principal{UserID: targetUserID, Username: username}
	if err := insertUserAudit(
		ctx, transaction, principal, "PASSWORD_RESET_COMPLETED", "USER", targetUserID,
		nil, map[string]any{"status": currentStatus}, now,
	); err != nil {
		return PasswordResetResult{}, err
	}
	result := PasswordResetResult{Status: "PASSWORD_CHANGED_ACCOUNT_DISABLED"}
	if currentStatus == "ENABLED" {
		if err := insertPreparedSession(
			ctx, transaction, prepared, targetUserID, sessionVersion, now,
		); err != nil {
			return PasswordResetResult{}, err
		}
		session := prepared.view(
			User{UserID: targetUserID, Username: username, DisplayName: displayName, Role: role},
			profileID, sessionVersion, now,
		)
		result.Session = &session
		result.Status = "AUTHENTICATED"
	}
	if err := transaction.Commit(); err != nil {
		return PasswordResetResult{}, fmt.Errorf("commit password reset: %w", err)
	}
	return result, nil
}

func (service *Service) RevokeAccountLink(
	ctx context.Context,
	principal authn.Principal,
	linkID string,
	expectedVersion int64,
	idempotencyKey string,
) (bool, error) {
	digest := operationDigest("deleteAdminAccountLink", principal.UserID, map[string]any{
		"accountLinkId":   linkID,
		"expectedVersion": expectedVersion,
	})
	now := service.now().UTC().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin account-link revocation: %w", err)
	}
	defer cleanup.Rollback(transaction)
	_, replayed, err := loadIdempotency(
		ctx, transaction, principal.UserID, "deleteAdminAccountLink", idempotencyKey, digest, now,
	)
	if err != nil || replayed {
		return replayed, err
	}
	var expiresAt, version int64
	var kind string
	var consumedAt, revokedAt sql.NullInt64
	if err := transaction.QueryRowContext(ctx, `
SELECT kind,expires_at_ms,consumed_at_ms,revoked_at_ms,version FROM account_links WHERE id=?
`, linkID).Scan(&kind, &expiresAt, &consumedAt, &revokedAt, &version); errors.Is(err, sql.ErrNoRows) {
		return false, ErrAccountLinkNotActive
	} else if err != nil {
		return false, fmt.Errorf("read account link: %w", err)
	}
	if version != expectedVersion {
		return false, ErrUserVersion
	}
	if accountLinkState(consumedAt, revokedAt, expiresAt, now) != "ACTIVE" {
		return false, ErrAccountLinkNotActive
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE account_links
SET revoked_at_ms=?,revoked_by_kind='USER',revoked_by_user_id=?,version=version+1
WHERE id=? AND version=? AND consumed_at_ms IS NULL AND revoked_at_ms IS NULL AND expires_at_ms>?
`, now, principal.UserID, linkID, expectedVersion, now)
	if err != nil {
		return false, fmt.Errorf("revoke account link: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return false, ErrAccountLinkNotActive
	}
	action := "PASSWORD_RESET_REVOKED"
	if kind == "INVITATION" {
		action = "INVITATION_REVOKED"
	}
	if err := insertUserAudit(
		ctx, transaction, principal, action, "ACCOUNT_LINK", linkID,
		map[string]any{"state": "ACTIVE"}, map[string]any{"state": "REVOKED"}, now,
	); err != nil {
		return false, err
	}
	if err := storeIdempotency(
		ctx, transaction, principal.UserID, "deleteAdminAccountLink", idempotencyKey, digest,
		http.StatusNoContent, nil, now,
	); err != nil {
		return false, err
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit account-link revocation: %w", err)
	}
	return false, nil
}

//nolint:funlen,gocyclo // Closed link filters and derived states share one stable projection.
func (service *Service) ListAccountLinks(
	ctx context.Context,
	filter LinkListFilter,
) ([]AccountLink, error) {
	if filter.Kind != "INVITATION" && filter.Kind != "PASSWORD_RESET" {
		return nil, ErrAccountLinkUnavailable
	}
	if filter.State == "" {
		filter.State = "ACTIVE"
	}
	if filter.State != "ACTIVE" && filter.State != "CONSUMED" && filter.State != "REVOKED" &&
		filter.State != "EXPIRED" && filter.State != "ALL" {
		return nil, ErrAccountLinkUnavailable
	}
	now := service.now().UTC().UnixMilli()
	query := `
SELECT link.id,link.kind,link.invited_role,link.target_user_id,
creator.id,creator.username,link.version,link.created_at_ms,link.expires_at_ms,
link.consumed_at_ms,link.revoked_at_ms
FROM account_links link
JOIN users creator ON creator.id=link.created_by_user_id
WHERE link.kind=?`
	arguments := []any{filter.Kind}
	if filter.TargetUserID != "" {
		query += " AND link.target_user_id=?"
		arguments = append(arguments, filter.TargetUserID)
	}
	switch filter.State {
	case "ACTIVE":
		query += " AND link.consumed_at_ms IS NULL AND link.revoked_at_ms IS NULL AND link.expires_at_ms>?"
		arguments = append(arguments, now)
	case "CONSUMED":
		query += " AND link.consumed_at_ms IS NOT NULL"
	case "REVOKED":
		query += " AND link.consumed_at_ms IS NULL AND link.revoked_at_ms IS NOT NULL"
	case "EXPIRED":
		query += ` AND link.consumed_at_ms IS NULL AND link.revoked_at_ms IS NULL
AND link.expires_at_ms<=?`
		arguments = append(arguments, now)
	}
	if filter.AfterID != "" {
		query += " AND (link.created_at_ms<? OR (link.created_at_ms=? AND link.id<?))"
		arguments = append(arguments, filter.AfterAtMS, filter.AfterAtMS, filter.AfterID)
	}
	query += " ORDER BY link.created_at_ms DESC,link.id DESC LIMIT ?"
	arguments = append(arguments, filter.Limit)
	rows, err := service.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list account links: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]AccountLink, 0, filter.Limit)
	for rows.Next() {
		var item AccountLink
		var role, target sql.NullString
		var creatorID, creatorUsername string
		var consumed, revoked sql.NullInt64
		if err := rows.Scan(
			&item.AccountLinkID, &item.Kind, &role, &target, &creatorID, &creatorUsername,
			&item.Version, &item.CreatedAtMS, &item.ExpiresAtMS, &consumed, &revoked,
		); err != nil {
			return nil, fmt.Errorf("scan account link: %w", err)
		}
		item.Role, item.TargetUserID = nil, nil
		if role.Valid {
			item.Role = role.String
		}
		if target.Valid {
			item.TargetUserID = target.String
		}
		item.CreatedBy = map[string]any{"userId": creatorID, "username": creatorUsername}
		item.ConsumedAtMS, item.RevokedAtMS = nil, nil
		if consumed.Valid {
			item.ConsumedAtMS = consumed.Int64
		}
		if revoked.Valid {
			item.RevokedAtMS = revoked.Int64
		}
		item.State = accountLinkState(consumed, revoked, item.ExpiresAtMS, now)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate account links: %w", err)
	}
	return items, nil
}
