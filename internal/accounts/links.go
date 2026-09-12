package accounts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	accountpersistence "retrom/internal/persistence/accounts"
	accountservice "retrom/internal/service/accounts"

	"retrom/internal/dbexec"

	"retrom/internal/persistence/recordstore"

	"github.com/google/uuid"

	"retrom/internal/authn"
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
	defer dbexec.Rollback(transaction)
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
		map[string]any{"kind": "INVITATION", "role": role, "expiresAtMs": expires}, now,
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

func (service *Service) links() *accountservice.LinkService {
	return accountservice.NewLinks(
		accountpersistence.NewLinks(
			service.database,
		),
		service.credentials,
		func() time.Time {
			return service.now()
		},
	)
}

func (service *Service) InspectAccountLink(ctx context.Context, kind, token string) (LinkInspection, error) {
	value, err := service.links().Inspect(ctx, kind, token)
	if err != nil {
		return LinkInspection{}, fmt.Errorf("inspect account link: %w", err)
	}
	result := LinkInspection{Kind: value.Kind, ExpiresAtMS: value.ExpiresAtMS}
	if value.Role != nil {
		result.Role = *value.Role
	}
	if value.Username != nil {
		result.Username = *value.Username
	}
	return result, nil
}

// Invitation consumption must create identity, credential, session, and audit atomically.
func (service *Service) AcceptInvitation(
	ctx context.Context,
	request AcceptInvitationRequest,
) (Session, error) {
	input, err := service.prepareInvitationAcceptance(ctx, request)
	if err != nil {
		return Session{}, err
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, fmt.Errorf("begin invitation acceptance: %w", err)
	}
	defer dbexec.Rollback(transaction)
	role, err := readActiveInvitation(ctx, transaction, input.linkID, input.now)
	if err != nil {
		return Session{}, err
	}
	if err := ensureUsernameAvailable(ctx, transaction, input.username); err != nil {
		return Session{}, err
	}
	if err := persistInvitedIdentity(ctx, transaction, input, role); err != nil {
		return Session{}, err
	}
	if err := insertPreparedSession(ctx, transaction, input.session, input.userID, 1, input.now); err != nil {
		return Session{}, err
	}
	principal := authn.Principal{UserID: input.userID, Username: input.username}
	if err := insertUserAudit(
		ctx, transaction, principal, "INVITATION_ACCEPTED", "ACCOUNT_LINK", input.linkID.String(),
		map[string]any{"role": role, "status": "CONSUMED", "userId": input.userID}, input.now,
	); err != nil {
		return Session{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Session{}, fmt.Errorf("commit invitation acceptance: %w", err)
	}
	return input.session.view(
		User{UserID: input.userID, Username: input.username, DisplayName: input.displayName, Role: role},
		input.profileID, 1, input.now,
	), nil
}

type invitationAcceptance struct {
	linkID                uuid.UUID
	username, displayName string
	encodedPassword       string
	session               preparedSession
	userID, profileID     string
	now                   int64
}

func (service *Service) prepareInvitationAcceptance(
	ctx context.Context,
	request AcceptInvitationRequest,
) (invitationAcceptance, error) {
	linkID, valid := service.credentials.ParseAccountLinkToken("INVITATION", request.Token)
	if !valid {
		return invitationAcceptance{}, ErrAccountLinkUnavailable
	}
	username, err := authn.NormalizeUsername(request.Username)
	if err != nil {
		return invitationAcceptance{}, fmt.Errorf("normalize invited username: %w", err)
	}
	displayName, err := authn.NormalizeDisplayName(request.DisplayName)
	if err != nil {
		return invitationAcceptance{}, fmt.Errorf("normalize invited display name: %w", err)
	}
	password, err := authn.ValidatePassword(
		request.Password, request.PasswordConfirmation, username, displayName, service.blocklist,
	)
	if err != nil {
		return invitationAcceptance{}, fmt.Errorf("validate invited password: %w", err)
	}
	encoded, err := service.hasher.Hash(ctx, password)
	if err != nil {
		return invitationAcceptance{}, fmt.Errorf("hash invited password: %w", err)
	}
	prepared, err := service.prepareSession()
	if err != nil {
		return invitationAcceptance{}, err
	}
	return invitationAcceptance{
		linkID: linkID, username: username, displayName: displayName, encodedPassword: encoded,
		session: prepared, userID: newID(), profileID: newID(), now: service.now().UTC().UnixMilli(),
	}, nil
}

func readActiveInvitation(
	ctx context.Context,
	transaction *sql.Tx,
	linkID uuid.UUID,
	now int64,
) (string, error) {
	var role string
	var expiresAt int64
	var consumedAt, revokedAt sql.NullInt64
	if err := transaction.QueryRowContext(ctx, `
SELECT invited_role,expires_at_ms,consumed_at_ms,revoked_at_ms
FROM account_links WHERE id=? AND kind='INVITATION'
`, linkID.String()).Scan(&role, &expiresAt, &consumedAt, &revokedAt); err != nil ||
		accountLinkState(consumedAt, revokedAt, expiresAt, now) != "ACTIVE" {
		return "", ErrAccountLinkUnavailable
	}
	return role, nil
}

func ensureUsernameAvailable(ctx context.Context, transaction *sql.Tx, username string) error {
	var usernameExists int
	if err := transaction.QueryRowContext(ctx, `
SELECT EXISTS(SELECT 1 FROM users WHERE username=?)
`, username).Scan(&usernameExists); err != nil {
		return fmt.Errorf("check invited username: %w", err)
	}
	if usernameExists != 0 {
		return ErrUsernameUnavailable
	}
	return nil
}

func persistInvitedIdentity(
	ctx context.Context,
	transaction *sql.Tx,
	input invitationAcceptance,
	role string,
) error {
	if _, err := recordstore.CreateProfiles(ctx, transaction, `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,?,?)
`, input.profileID, input.displayName, input.now); err != nil {
		return fmt.Errorf("create invited profile: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,'ENABLED',?,?)
ON CONFLICT(username) DO NOTHING
`, input.userID, input.profileID, input.username, input.displayName, role, input.now, input.now)
	if err != nil {
		return fmt.Errorf("create invited user: %w", err)
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("read invited user insert result: %w", rowsErr)
	} else if changed != 1 {
		return ErrUsernameUnavailable
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO user_credentials(user_id,password_hash,password_scheme,password_changed_at_ms,created_at_ms)
VALUES(?,?,'ARGON2ID_V1',?,?)
`, input.userID, input.encodedPassword, input.now, input.now); err != nil {
		return fmt.Errorf("create invited credential: %w", err)
	}
	result, err = recordstore.UpdateAccountLinks(ctx, transaction, recordstore.Update{
		Set: `consumed_at_ms=?,consumed_by_user_id=?,version=version+1`,
		Scope: recordstore.Scope{
			Where: `id=? AND consumed_at_ms IS NULL AND revoked_at_ms IS NULL AND expires_at_ms>?`,
			Args:  []any{input.linkID.String(), input.now},
		},
		Values: []any{input.now, input.userID},
	})
	if err != nil {
		return fmt.Errorf("consume invitation: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrAccountLinkUnavailable
	}
	return nil
}

// Closed transaction preserves exact version and idempotency semantics.
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
	defer dbexec.Rollback(transaction)
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
	if err := validatePasswordResetTarget(ctx, transaction, targetUserID, expectedVersion); err != nil {
		return AccountLink{}, false, err
	}
	result, err := recordstore.UpdateUsers(ctx, transaction, recordstore.Update{
		Set: `version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND status!='DELETED'`,
			Args:  []any{targetUserID, expectedVersion},
		},
		Values: []any{now},
	})
	if err != nil {
		return AccountLink{}, false, fmt.Errorf("version password-reset target: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return AccountLink{}, false, ErrUserVersion
	}
	if _, err := recordstore.UpdateAccountLinks(ctx, transaction, recordstore.Update{
		Set: `revoked_at_ms=?,revoked_by_kind='SYSTEM',version=version+1`,
		Scope: recordstore.Scope{
			Where: `
kind='PASSWORD_RESET' AND target_user_id=?
AND consumed_at_ms IS NULL AND revoked_at_ms IS NULL AND expires_at_ms>?
`,
			Args: []any{targetUserID, now},
		},
		Values: []any{now},
	}); err != nil {
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
		map[string]any{"targetUserId": targetUserID, "expiresAtMs": expires}, now,
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

func validatePasswordResetTarget(
	ctx context.Context,
	transaction *sql.Tx,
	targetUserID string,
	expectedVersion int64,
) error {
	var status string
	var currentVersion int64
	err := transaction.QueryRowContext(ctx, `
SELECT status,version FROM users WHERE id=?
`, targetUserID).Scan(&status, &currentVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUserNotFound
	}
	if err != nil {
		return fmt.Errorf("read password-reset target: %w", err)
	}
	if status == "DELETED" {
		return ErrUserDeleted
	}
	if currentVersion != expectedVersion {
		return ErrUserVersion
	}
	return nil
}

// Capability consumption, password rotation, revocation, and optional session are atomic.
func (service *Service) CompletePasswordReset(
	ctx context.Context,
	request CompletePasswordResetRequest,
) (PasswordResetResult, error) {
	input, err := service.preparePasswordReset(ctx, request)
	if err != nil {
		return PasswordResetResult{}, err
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return PasswordResetResult{}, fmt.Errorf("begin password reset: %w", err)
	}
	defer dbexec.Rollback(transaction)
	return service.completePasswordReset(ctx, transaction, input)
}

type passwordResetInput struct {
	linkID                       uuid.UUID
	targetUserID, username       string
	displayName, encodedPassword string
	prepared                     preparedSession
	now                          int64
}

func (service *Service) preparePasswordReset(
	ctx context.Context,
	request CompletePasswordResetRequest,
) (passwordResetInput, error) {
	linkID, valid := service.credentials.ParseAccountLinkToken("PASSWORD_RESET", request.Token)
	if !valid {
		return passwordResetInput{}, ErrAccountLinkUnavailable
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
		return passwordResetInput{}, ErrAccountLinkUnavailable
	}
	password, err := authn.ValidatePassword(
		request.Password, request.PasswordConfirmation, username, displayName, service.blocklist,
	)
	if err != nil {
		return passwordResetInput{}, fmt.Errorf("validate reset password: %w", err)
	}
	encoded, err := service.hasher.Hash(ctx, password)
	if err != nil {
		return passwordResetInput{}, fmt.Errorf("hash reset password: %w", err)
	}
	prepared := preparedSession{}
	if status == "ENABLED" {
		prepared, err = service.prepareSession()
		if err != nil {
			return passwordResetInput{}, err
		}
	}
	return passwordResetInput{
		linkID: linkID, targetUserID: targetUserID, username: username, displayName: displayName,
		encodedPassword: encoded, prepared: prepared, now: service.now().UTC().UnixMilli(),
	}, nil
}

func (service *Service) completePasswordReset(
	ctx context.Context,
	transaction *sql.Tx,
	input passwordResetInput,
) (PasswordResetResult, error) {
	var role, profileID, currentStatus string
	var sessionVersion int64
	if err := transaction.QueryRowContext(ctx, `
UPDATE users SET session_version=session_version+1,version=version+1,updated_at_ms=?
WHERE id=? AND status!='DELETED'
RETURNING role,profile_id,status,session_version
`, input.now, input.targetUserID).Scan(&role, &profileID, &currentStatus, &sessionVersion); err != nil {
		return PasswordResetResult{}, ErrAccountLinkUnavailable
	}
	consume, err := recordstore.UpdateAccountLinks(ctx, transaction, recordstore.Update{
		Set: `consumed_at_ms=?,consumed_by_user_id=?,version=version+1`,
		Scope: recordstore.Scope{
			Where: `
id=? AND kind='PASSWORD_RESET'
AND consumed_at_ms IS NULL AND revoked_at_ms IS NULL AND expires_at_ms>?
`,
			Args: []any{input.linkID.String(), input.now},
		},
		Values: []any{input.now, input.targetUserID},
	})
	if err != nil {
		return PasswordResetResult{}, fmt.Errorf("consume password-reset link: %w", err)
	}
	if changed, _ := consume.RowsAffected(); changed != 1 {
		return PasswordResetResult{}, ErrAccountLinkUnavailable
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE user_credentials SET password_hash=?,password_changed_at_ms=? WHERE user_id=?
`, input.encodedPassword, input.now, input.targetUserID); err != nil {
		return PasswordResetResult{}, fmt.Errorf("replace reset credential: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE auth_sessions SET revoked_at_ms=?,revoked_reason='PASSWORD_RESET'
WHERE user_id=? AND revoked_at_ms IS NULL
`, input.now, input.targetUserID); err != nil {
		return PasswordResetResult{}, fmt.Errorf("revoke reset sessions: %w", err)
	}
	if input.username == "test" {
		_, _ = recordstore.UpdateInstanceState(ctx, transaction, recordstore.Update{
			Set: `test_default_password_active=0,version=version+1,updated_at_ms=?`,
			Scope: recordstore.Scope{
				Where: `id=1 AND test_default_password_active=1`,
			},
			Values: []any{input.now},
		})
	}
	principal := authn.Principal{UserID: input.targetUserID, Username: input.username}
	if err := insertUserAudit(
		ctx, transaction, principal, "PASSWORD_RESET_COMPLETED", "USER", input.targetUserID,
		map[string]any{"status": currentStatus}, input.now,
	); err != nil {
		return PasswordResetResult{}, err
	}
	result := PasswordResetResult{Status: "PASSWORD_CHANGED_ACCOUNT_DISABLED"}
	if currentStatus == "ENABLED" {
		if err := insertPreparedSession(
			ctx, transaction, input.prepared, input.targetUserID, sessionVersion, input.now,
		); err != nil {
			return PasswordResetResult{}, err
		}
		session := input.prepared.view(
			User{
				UserID: input.targetUserID, Username: input.username, DisplayName: input.displayName, Role: role,
			},
			profileID, sessionVersion, input.now,
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
	id string,
	version int64,
	key string,
) (bool, error) {
	replayed, err := service.links().Revoke(ctx, principal.UserID, id, version, key)
	if err != nil {
		return false, fmt.Errorf("revoke account link: %w", err)
	}
	return replayed, nil
}

func (service *Service) ListAccountLinks(ctx context.Context, filter LinkListFilter) ([]AccountLink, error) {
	values, err := service.links().List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list account links: %w", err)
	}
	result := make([]AccountLink, len(values))
	for index, value := range values {
		result[index] = legacyAccountLink(value)
	}
	return result, nil
}

func legacyAccountLink(value accountservice.AccountLink) AccountLink {
	result := AccountLink{
		AccountLinkID:   value.AccountLinkID,
		Kind:            value.Kind,
		State:           value.State,
		Version:         value.Version,
		CreatedAtMS:     value.CreatedAtMS,
		ExpiresAtMS:     value.ExpiresAtMS,
		TargetVersion:   value.TargetVersion,
		CapabilityToken: value.CapabilityToken,
	}
	if value.Role != nil {
		result.Role = *value.Role
	}
	if value.TargetUserID != nil {
		result.TargetUserID = *value.TargetUserID
	}
	if value.CreatedBy != nil {
		result.CreatedBy = map[string]any{"userId": value.CreatedBy.UserID, "username": value.CreatedBy.Username}
	}
	if value.ConsumedAtMS != nil {
		result.ConsumedAtMS = *value.ConsumedAtMS
	}
	if value.RevokedAtMS != nil {
		result.RevokedAtMS = *value.RevokedAtMS
	}
	return result
}
