package accounts

import (
	"context"
	"fmt"
	"time"
)

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
