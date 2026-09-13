package accounts

import (
	"errors"
	"strings"
)

var (
	ErrUserVersion       = errors.New("USER_VERSION_CONFLICT")
	ErrUserNoChange      = errors.New("USER_NO_STATE_CHANGE")
	ErrUserSelfChange    = errors.New("USER_SELF_CHANGE_FORBIDDEN")
	ErrLastAdmin         = errors.New("LAST_ENABLED_ADMIN")
	ErrUserDeleted       = errors.New("USER_ALREADY_DELETED")
	ErrUserTransition    = errors.New("USER_INVALID_TRANSITION")
	ErrConfirmation      = errors.New("CONFIRMATION_MISMATCH")
	ErrRoleConfirmation  = errors.New("ADMIN_ROLE_CONFIRMATION_REQUIRED")
	ErrIdempotencyReused = errors.New("IDEMPOTENCY_KEY_REUSED")
)

func resolveUserChange(before AdminUser, patch UserPatch, self bool) (UserChange, error) {
	role, err := resolvedRole(before.Role, patch.Role)
	if err != nil {
		return UserChange{}, err
	}
	status, err := resolvedStatus(before.Status, patch.Status)
	if err != nil {
		return UserChange{}, err
	}
	if role == before.Role && status == before.Status {
		return UserChange{}, ErrUserNoChange
	}
	if (patch.Role != nil && role == "ADMIN") != patch.ConfirmAdminRole {
		return UserChange{}, ErrRoleConfirmation
	}
	downgraded := before.Role == "ADMIN" && role == "USER"
	disabled := before.Status == "ENABLED" && status == "DISABLED"
	if self && (downgraded || disabled) {
		return UserChange{}, ErrUserSelfChange
	}
	security := UserSecurity{
		Reason:       "ROLE_CHANGED",
		Sessions:     before.Role != role || disabled,
		CreatedLinks: downgraded || disabled,
		TargetLinks:  disabled,
		Launches:     disabled,
	}
	if disabled {
		security.Reason = "USER_DISABLED"
	}
	return UserChange{Role: role, Status: status, Security: security}, nil
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

func validateManagedUser(user ManagedUser, found bool, version int64) error {
	if !found {
		return ErrUserNotFound
	}
	if user.User.Status == "DELETED" {
		return ErrUserDeleted
	}
	if user.User.Version != version {
		return ErrUserVersion
	}
	return nil
}

func validateUserDeletion(before AdminUser, actorID, confirmation string) error {
	if before.UserID == actorID {
		return ErrUserSelfChange
	}
	if confirmation != before.Username || strings.TrimSpace(confirmation) != confirmation {
		return ErrConfirmation
	}
	return nil
}

func removesEnabledAdmin(before AdminUser, role, status string) bool {
	return before.Role == "ADMIN" && before.Status == "ENABLED" && (role != "ADMIN" || status != "ENABLED")
}
