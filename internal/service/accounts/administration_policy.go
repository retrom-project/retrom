package accounts

import (
	"strings"

	model "retrom/internal/model/accounts"
)

func resolveUserChange(before model.AdminUser, patch model.UserPatch, self bool) (model.UserChange, error) {
	role, err := resolvedRole(before.Role, patch.Role)
	if err != nil {
		return model.UserChange{}, err
	}
	status, err := resolvedStatus(before.Status, patch.Status)
	if err != nil {
		return model.UserChange{}, err
	}
	if role == before.Role && status == before.Status {
		return model.UserChange{}, model.ErrUserNoChange
	}
	if (patch.Role != nil && role == "ADMIN") != patch.ConfirmAdminRole {
		return model.UserChange{}, model.ErrRoleConfirmation
	}
	downgraded := before.Role == "ADMIN" && role == "USER"
	disabled := before.Status == "ENABLED" && status == "DISABLED"
	if self && (downgraded || disabled) {
		return model.UserChange{}, model.ErrUserSelfChange
	}
	security := model.UserSecurity{
		Reason:       "ROLE_CHANGED",
		Sessions:     before.Role != role || disabled,
		CreatedLinks: downgraded || disabled,
		TargetLinks:  disabled,
		Launches:     disabled,
	}
	if disabled {
		security.Reason = "USER_DISABLED"
	}
	return model.UserChange{Role: role, Status: status, Security: security}, nil
}

func resolvedRole(current string, requested *string) (string, error) {
	if requested == nil {
		return current, nil
	}
	if *requested != "ADMIN" && *requested != "USER" {
		return "", model.ErrRoleConfirmation
	}
	return *requested, nil
}

func resolvedStatus(current string, requested *string) (string, error) {
	if requested == nil {
		return current, nil
	}
	if *requested != "ENABLED" && *requested != "DISABLED" {
		return "", model.ErrUserTransition
	}
	return *requested, nil
}

func validateManagedUser(user model.ManagedUser, found bool, version int64) error {
	if !found {
		return model.ErrUserNotFound
	}
	if user.User.Status == "DELETED" {
		return model.ErrUserDeleted
	}
	if user.User.Version != version {
		return model.ErrUserVersion
	}
	return nil
}

func validateUserDeletion(before model.AdminUser, actorID, confirmation string) error {
	if before.UserID == actorID {
		return model.ErrUserSelfChange
	}
	if confirmation != before.Username || strings.TrimSpace(confirmation) != confirmation {
		return model.ErrConfirmation
	}
	return nil
}

func removesEnabledAdmin(before model.AdminUser, role, status string) bool {
	return before.Role == "ADMIN" && before.Status == "ENABLED" && (role != "ADMIN" || status != "ENABLED")
}
