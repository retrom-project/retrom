package accounts

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	accountservice "retrom/internal/service/accounts"

	"retrom/internal/authn"
)

var (
	ErrAccountLinkUnavailable = accountservice.ErrAccountLinkUnavailable
	ErrAccountLinkNotActive   = accountservice.ErrAccountLinkNotActive
	ErrUsernameUnavailable    = errors.New("USERNAME_UNAVAILABLE")
	ErrUserNotFound           = accountservice.ErrUserNotFound
	ErrUserQuery              = accountservice.ErrUserQuery
	ErrUserVersion            = accountservice.ErrUserVersion
	ErrUserNoChange           = accountservice.ErrUserNoChange
	ErrUserSelfChange         = accountservice.ErrUserSelfChange
	ErrLastAdmin              = accountservice.ErrLastAdmin
	ErrUserDeleted            = accountservice.ErrUserDeleted
	ErrUserTransition         = accountservice.ErrUserTransition
	ErrConfirmation           = accountservice.ErrConfirmation
	ErrRoleConfirmation       = accountservice.ErrRoleConfirmation
	ErrIdempotencyReused      = accountservice.ErrIdempotencyReused
)

type AdminUser = accountservice.AdminUser

type AccountLink struct {
	AccountLinkID   string `json:"accountLinkId"`
	Kind            string `json:"kind"`
	Role            any    `json:"role"`
	TargetUserID    any    `json:"targetUserId"`
	CreatedBy       any    `json:"createdBy"`
	State           string `json:"state"`
	Version         int64  `json:"version"`
	CreatedAtMS     int64  `json:"createdAtMs"`
	ExpiresAtMS     int64  `json:"expiresAtMs"`
	ConsumedAtMS    any    `json:"consumedAtMs"`
	RevokedAtMS     any    `json:"revokedAtMs"`
	TargetVersion   int64  `json:"targetUserVersion,omitempty"`
	CapabilityToken string `json:"-"`
}

type LinkInspection struct {
	Kind        string `json:"kind"`
	Role        any    `json:"role"`
	Username    any    `json:"username"`
	ExpiresAtMS int64  `json:"expiresAtMs"`
}

type UserPatch = accountservice.UserPatch

type UserListFilter = accountservice.UserListFilter

type LinkListFilter = accountservice.LinkListFilter

func operationDigest(operation, principalID string, value any) string {
	encoded, _ := json.Marshal(map[string]any{
		"operationId": operation,
		"principalId": principalID,
		"value":       value,
	})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func loadIdempotency(
	ctx context.Context,
	transaction *sql.Tx,
	principalID, operation, key, digest string,
	now int64,
) ([]byte, bool, error) {
	var storedDigest string
	var status int
	var body []byte
	err := transaction.QueryRowContext(ctx, `
SELECT request_digest,http_status,response_body
FROM idempotency_records
WHERE principal_id=? AND operation_id=? AND key=? AND expires_at_ms>?
`, principalID, operation, key, now).Scan(&storedDigest, &status, &body)
	if err == nil {
		if storedDigest != digest {
			return nil, false, ErrIdempotencyReused
		}
		return body, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("read account idempotency: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
DELETE FROM idempotency_records
WHERE principal_id=? AND operation_id=? AND key=? AND expires_at_ms<=?
	`, principalID, operation, key, now); err != nil {
		return nil, false, fmt.Errorf("expire account idempotency: %w", err)
	}
	return nil, false, nil
}

func storeIdempotency(
	ctx context.Context,
	transaction *sql.Tx,
	principalID, operation, key, digest string,
	status int,
	body []byte,
	now int64,
) error {
	if body == nil {
		body = []byte{}
	}
	_, err := transaction.ExecContext(ctx, `
INSERT INTO idempotency_records(
principal_id,operation_id,key,request_digest,http_status,response_headers_json,response_body,
created_at_ms,expires_at_ms)
VALUES(?,?,?,?,?,'{}',?,?,?)
`, principalID, operation, key, digest, status, body, now, now+int64(24*time.Hour/time.Millisecond))
	if err != nil {
		return fmt.Errorf("store account idempotency: %w", err)
	}
	return nil
}

func insertUserAudit(
	ctx context.Context,
	transaction *sql.Tx,
	actor authn.Principal,
	action, resourceType, resourceID string,
	after any,
	now int64,
) error {
	encoded, err := json.Marshal(after)
	if err != nil {
		return fmt.Errorf("encode account audit: %w", err)
	}
	afterJSON := string(encoded)

	_, err = transaction.ExecContext(ctx, `
INSERT INTO audit_events(
id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms)
VALUES(?,'USER',?,NULL,?,?,?,?,?,'{}',NULL,?)
`, newID(), actor.UserID, action, resourceType, resourceID, nil, afterJSON, now)
	if err != nil {
		return fmt.Errorf("insert account audit event: %w", err)
	}
	return nil
}
