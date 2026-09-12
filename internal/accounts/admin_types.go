package accounts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

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
