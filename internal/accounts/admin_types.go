package accounts

import (
	accountservice "retrom/internal/service/accounts"
)

var (
	ErrAccountLinkUnavailable = accountservice.ErrAccountLinkUnavailable
	ErrAccountLinkNotActive   = accountservice.ErrAccountLinkNotActive
	ErrUsernameUnavailable    = accountservice.ErrUsernameUnavailable
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
