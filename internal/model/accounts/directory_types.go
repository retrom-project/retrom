package accounts

import (
	"context"
	"errors"
)

var (
	ErrUserNotFound = errors.New("USER_NOT_FOUND")
	ErrUserQuery    = errors.New("USER_QUERY_INVALID")
)

type AdminUser struct {
	UserID             string `json:"userId"`
	Username           string `json:"username"`
	DisplayName        string `json:"displayName"`
	Role               string `json:"role"`
	Status             string `json:"status"`
	Version            int64  `json:"version"`
	CreatedAtMS        int64  `json:"createdAtMs"`
	LastLoginAtMS      *int64 `json:"lastLoginAtMs"`
	ActiveSessionCount int64  `json:"activeSessionCount"`
}

type UserListFilter struct {
	Query, Role, Status, Sort string
	AfterValues               []string
	AfterID                   string
	Limit                     int
}

type UserCursor struct {
	ID, Username         string
	CreatedAt, LastLogin int64
	NeverLoggedIn        bool
}

type UserQuery struct {
	Text, Role, Status, Sort string
	Limit                    int
	Now                      int64
	After                    *UserCursor
}

type DirectoryRepository interface {
	Get(context.Context, string, int64) (AdminUser, bool, error)
	List(context.Context, UserQuery) ([]AdminUser, error)
}
