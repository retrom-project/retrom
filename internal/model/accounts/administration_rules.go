package accounts

import (
	"fmt"
	"strings"
)

// CheckAccountReplay validates an idempotency replay against an operation.
func CheckAccountReplay(replay AccountReplay, operation AccountOperation) error {
	if replay.Found && replay.Digest != operation.Digest {
		return fmt.Errorf("account replay: %w", ErrIdempotencyReused)
	}
	return nil
}

// ResolveUserChange computes the change to apply to a managed user. It is a
// pure validation function with no I/O.
func ResolveUserChange(before AdminUser, patch UserPatch, self bool) (UserChange, error) {
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

// ValidateManagedUser checks that a managed user can be modified.
func ValidateManagedUser(user ManagedUser, found bool, version int64) error {
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

// ValidateUserDeletion checks that a user deletion request is valid.
func ValidateUserDeletion(before AdminUser, actorID, confirmation string) error {
	if before.UserID == actorID {
		return ErrUserSelfChange
	}
	if confirmation != before.Username || strings.TrimSpace(confirmation) != confirmation {
		return ErrConfirmation
	}
	return nil
}

// RemovesEnabledAdmin checks whether a role/status change would remove an
// enabled admin.
func RemovesEnabledAdmin(before AdminUser, role, status string) bool {
	return before.Role == "ADMIN" && before.Status == "ENABLED" && (role != "ADMIN" || status != "ENABLED")
}
