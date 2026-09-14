package accounts

import "errors"

var (
	ErrAuthentication         = errors.New("AUTHENTICATION_FAILED")
	ErrAuthenticationNeeded   = errors.New("AUTHENTICATION_REQUIRED")
	ErrAccountLinkUnavailable = errors.New("ACCOUNT_LINK_UNAVAILABLE")
	ErrAccountLinkNotActive   = errors.New("ACCOUNT_LINK_NOT_ACTIVE")
	ErrConfirmation           = errors.New("CONFIRMATION_MISMATCH")
	ErrIdempotencyReused      = errors.New("IDEMPOTENCY_KEY_REUSED")
	ErrInitialization         = errors.New("INITIALIZATION_REQUIRED")
	ErrInitializationDone     = errors.New("INITIALIZATION_ALREADY_COMPLETED")
	ErrInitializationProof    = errors.New("INITIALIZATION_PROOF_INVALID")
	ErrInitializationState    = errors.New("INITIALIZATION_STATE_INVALID")
	ErrLastAdmin              = errors.New("LAST_ENABLED_ADMIN")
	ErrOfflineAdmin           = errors.New("OFFLINE_ADMIN_INVALID")
	ErrRateLimited            = errors.New("AUTH_RATE_LIMITED")
	ErrRoleConfirmation       = errors.New("ADMIN_ROLE_CONFIRMATION_REQUIRED")
	ErrTestCredential         = errors.New("TEST_DEFAULT_CREDENTIAL_ACTIVE")
	ErrUserDeleted            = errors.New("USER_ALREADY_DELETED")
	ErrUserNoChange           = errors.New("USER_NO_STATE_CHANGE")
	ErrUserSelfChange         = errors.New("USER_SELF_CHANGE_FORBIDDEN")
	ErrUserTransition         = errors.New("USER_INVALID_TRANSITION")
	ErrUserVersion            = errors.New("USER_VERSION_CONFLICT")
	ErrUsernameUnavailable    = errors.New("USERNAME_UNAVAILABLE")
)
