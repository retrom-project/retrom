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

	"retrom/internal/authn"
)

var (
	ErrAccountLinkUnavailable = errors.New("ACCOUNT_LINK_UNAVAILABLE")
	ErrAccountLinkNotActive   = errors.New("ACCOUNT_LINK_NOT_ACTIVE")
	ErrUsernameUnavailable    = errors.New("USERNAME_UNAVAILABLE")
	ErrUserNotFound           = errors.New("USER_NOT_FOUND")
	ErrUserVersion            = errors.New("USER_VERSION_CONFLICT")
	ErrUserNoChange           = errors.New("USER_NO_STATE_CHANGE")
	ErrUserSelfChange         = errors.New("USER_SELF_CHANGE_FORBIDDEN")
	ErrLastAdmin              = errors.New("LAST_ENABLED_ADMIN")
	ErrUserDeleted            = errors.New("USER_ALREADY_DELETED")
	ErrUserTransition         = errors.New("USER_INVALID_TRANSITION")
	ErrConfirmation           = errors.New("CONFIRMATION_MISMATCH")
	ErrRoleConfirmation       = errors.New("ADMIN_ROLE_CONFIRMATION_REQUIRED")
	ErrIdempotencyReused      = errors.New("IDEMPOTENCY_KEY_REUSED")
)

type AdminUser struct {
	UserID             string `json:"userId"`
	Username           string `json:"username"`
	DisplayName        string `json:"displayName"`
	Role               string `json:"role"`
	Status             string `json:"status"`
	Version            int64  `json:"version"`
	CreatedAtMS        int64  `json:"createdAtMs"`
	LastLoginAtMS      any    `json:"lastLoginAtMs"`
	ActiveSessionCount int64  `json:"activeSessionCount"`
}

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

type UserPatch struct {
	Role             *string
	Status           *string
	ConfirmAdminRole bool
}

type UserListFilter struct {
	Query       string
	Role        string
	Status      string
	Sort        string
	AfterValues []string
	AfterID     string
	Limit       int
}

type LinkListFilter struct {
	Kind         string
	TargetUserID string
	State        string
	AfterAtMS    int64
	AfterID      string
	Limit        int
}

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
	before, after any,
	now int64,
) error {
	beforeJSON, afterJSON := any(nil), any(nil)
	if before != nil {
		encoded, _ := json.Marshal(before)
		beforeJSON = string(encoded)
	}
	if after != nil {
		encoded, _ := json.Marshal(after)
		afterJSON = string(encoded)
	}
	_, err := transaction.ExecContext(ctx, `
INSERT INTO audit_events(
id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms)
VALUES(?,'USER',?,NULL,?,?,?,?,?,'{}',NULL,?)
`, newID(), actor.UserID, action, resourceType, resourceID, beforeJSON, afterJSON, now)
	if err != nil {
		return fmt.Errorf("insert account audit event: %w", err)
	}
	return nil
}
