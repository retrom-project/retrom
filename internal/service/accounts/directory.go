package accounts

import (
	"context"
	"errors"
	"fmt"
	"time"
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
type DirectoryService struct {
	repository DirectoryRepository
	now        func() time.Time
}

func NewDirectory(repository DirectoryRepository, now func() time.Time) *DirectoryService {
	return &DirectoryService{repository: repository, now: now}
}

func (service *DirectoryService) Get(ctx context.Context, id string) (AdminUser, error) {
	user, found, err := service.repository.Get(ctx, id, service.now().UnixMilli())
	if err != nil {
		return AdminUser{}, fmt.Errorf("read admin user: %w", err)
	}
	if !found {
		return AdminUser{}, ErrUserNotFound
	}
	return presentAdminUser(user), nil
}

func (service *DirectoryService) List(ctx context.Context, filter UserListFilter) ([]AdminUser, error) {
	query, err := directoryQuery(filter)
	if err != nil {
		return nil, err
	}
	query.Now = service.now().UnixMilli()
	users, err := service.repository.List(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list admin users: %w", err)
	}
	result := make([]AdminUser, len(users))
	for i, user := range users {
		result[i] = presentAdminUser(user)
	}
	return result, nil
}

func presentAdminUser(user AdminUser) AdminUser {
	if user.Status == "DELETED" {
		user.DisplayName = "已删除用户"
	}
	return user
}
