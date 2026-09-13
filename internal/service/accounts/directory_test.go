package accounts

import (
	"context"
	"errors"
	"testing"
	"time"
)

type directoryMemory struct {
	query UserQuery
	calls int
	user  AdminUser
	cause error
}

func (memory *directoryMemory) Get(context.Context, string, int64) (AdminUser, bool, error) {
	return memory.user, true, memory.cause
}

func (memory *directoryMemory) List(_ context.Context, query UserQuery) ([]AdminUser, error) {
	memory.query = query
	memory.calls++
	return []AdminUser{memory.user}, memory.cause
}

func TestDirectoryRejectsMalformedPaginationBeforeQuery(t *testing.T) {
	for _, filter := range []UserListFilter{
		{Sort: "LAST_LOGIN_DESC", AfterID: "user", AfterValues: []string{"1"}},
		{Sort: "CREATED_DESC", AfterValues: []string{"1"}},
		{Sort: "LAST_LOGIN_DESC", AfterID: "user", AfterValues: []string{"-2", "1"}},
		{Sort: "CREATED_DESC", AfterID: "user", AfterValues: []string{"not-time"}},
		{Limit: -1},
		{Limit: 102},
		{Query: "bad\x00query"},
		{Role: "ROOT"},
	} {
		memory := &directoryMemory{}
		_, err := NewDirectory(memory, time.Now).List(t.Context(), filter)
		if !errors.Is(err, ErrUserQuery) || memory.calls != 0 {
			t.Fatalf("invalid filter %+v: %v", filter, err)
		}
	}
}

func TestDirectoryNormalizesFilterAndProjectsDeletedUser(t *testing.T) {
	memory := &directoryMemory{user: AdminUser{UserID: "deleted", Status: "DELETED", DisplayName: "old name"}}
	items, err := NewDirectory(memory, func() time.Time { return time.UnixMilli(100) }).List(t.Context(), UserListFilter{Query: "  Cafe\u0301  ", Sort: "LAST_LOGIN_DESC", AfterID: "user", AfterValues: []string{"-1", "42"}, Limit: 51})
	if err != nil {
		t.Fatal(err)
	}
	query := memory.query
	if query.Text != "Café" || query.Status != "NON_DELETED" || query.Now != 100 || query.After == nil || !query.After.NeverLoggedIn || query.After.CreatedAt != 42 {
		t.Fatalf("directory query: %+v", query)
	}
	if len(items) != 1 || items[0].DisplayName != "已删除用户" || memory.user.DisplayName != "old name" {
		t.Fatalf("deleted projection: %+v", items)
	}
}

func TestDirectoryPreservesStorageFailure(t *testing.T) {
	memory := &directoryMemory{cause: context.Canceled}
	_, err := NewDirectory(memory, time.Now).List(t.Context(), UserListFilter{Limit: 51})
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrUserQuery) {
		t.Fatalf("storage failure converted to invalid query: %v", err)
	}
}
