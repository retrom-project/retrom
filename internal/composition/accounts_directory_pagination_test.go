package composition

import (
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"retrom/internal/config"
	accountservice "retrom/internal/service/accounts"
)

func TestUserDirectoryPaginationKeepsTiesAndNullLogins(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	admin := authenticatedTestAdmin(t, fixture)
	for _, name := range []string{"alice", "bob", "carol", "dave"} {
		acceptFixtureInvitation(t, fixture, admin.Principal, "USER", name, name)
	}
	for index, name := range []string{"alice", "bob", "carol", "dave"} {
		var lastLogin *int64
		if index < 2 {
			value := int64(100)
			lastLogin = &value
		}
		_, err := fixture.database.SQL.ExecContext(t.Context(), `UPDATE users SET created_at_ms=?,last_login_at_ms=? WHERE username=?`, 50, lastLogin, name)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, sort := range []string{"CREATED_DESC", "USERNAME_ASC", "LAST_LOGIN_DESC"} {
		t.Run(sort, func(t *testing.T) {
			all, err := fixture.service.ListUsers(t.Context(), accountservice.UserListFilter{Role: "USER", Sort: sort, Limit: 101})
			if err != nil {
				t.Fatal(err)
			}
			ids := pageDirectoryUsers(t, fixture, sort, len(all))
			want := make([]string, len(all))
			for index, user := range all {
				want[index] = user.UserID
			}
			if len(want) != 4 || !reflect.DeepEqual(ids, want) {
				t.Fatalf("cursor lost or duplicated users: got %v want %v", ids, want)
			}
		})
	}
}

func directoryTestCursor(sort string, user accountservice.AdminUser) []string {
	switch sort {
	case "USERNAME_ASC":
		return []string{user.Username}
	case "LAST_LOGIN_DESC":
		last := int64(-1)
		if user.LastLoginAtMS != nil {
			last = *user.LastLoginAtMS
		}
		return []string{strconv.FormatInt(last, 10), strconv.FormatInt(user.CreatedAtMS, 10)}
	default:
		return []string{fmt.Sprint(user.CreatedAtMS)}
	}
}

func pageDirectoryUsers(t *testing.T, fixture accountFixture, sort string, count int) []string {
	t.Helper()
	filter := accountservice.UserListFilter{Role: "USER", Sort: sort, Limit: 1}
	ids := make([]string, 0, count)
	for range count + 1 {
		page, err := fixture.service.ListUsers(t.Context(), filter)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		user := page[0]
		ids = append(ids, user.UserID)
		filter.AfterID = user.UserID
		filter.AfterValues = directoryTestCursor(sort, user)
	}
	return ids
}
