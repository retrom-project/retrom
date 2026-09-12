package accounts

import (
	"testing"

	"retrom/internal/config"
)

func TestUserDirectoryRejectsIncompleteCursor(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	authenticatedTestAdmin(t, fixture)
	_, err := fixture.service.ListUsers(t.Context(), UserListFilter{Sort: "LAST_LOGIN_DESC", AfterID: "user", AfterValues: []string{"1"}, Limit: 51})
	if err == nil {
		t.Fatal("incomplete cursor silently restarted user listing")
	}
}
