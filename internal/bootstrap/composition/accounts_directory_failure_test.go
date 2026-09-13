package composition

import (
	"testing"

	"retrom/internal/bootstrap/config"
	accountservice "retrom/internal/service/accounts"
)

func TestUserDirectoryRejectsIncompleteCursor(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	authenticatedTestAdmin(t, fixture)
	_, err := fixture.service.ListUsers(t.Context(), accountservice.UserListFilter{Sort: "LAST_LOGIN_DESC", AfterID: "user", AfterValues: []string{"1"}, Limit: 51})
	if err == nil {
		t.Fatal("incomplete cursor silently restarted user listing")
	}
}
