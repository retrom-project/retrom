package accounts

import (
	"errors"
	"testing"
	"time"

	"retrom/internal/config"
)

func TestSessionRefreshFailureCannotReportExtendedSession(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	session := authenticatedTestAdmin(t, fixture)
	advanced := fixture.now.Add(6 * time.Minute)
	fixture.service.now = func() time.Time {
		if _, err := fixture.database.SQL.ExecContext(t.Context(), `DROP TABLE auth_sessions`); err != nil {
			t.Fatal(err)
		}
		return advanced
	}
	_, err := fixture.service.Authenticate(t.Context(), session.CookieToken)
	if err == nil {
		t.Fatal("failed session refresh reported an extended authenticated session")
	}
}

func TestLoginStorageFailureIsNotReportedAsBadPassword(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	authenticatedTestAdmin(t, fixture)
	if _, err := fixture.database.SQL.ExecContext(t.Context(), `DROP TABLE user_credentials`); err != nil {
		t.Fatal(err)
	}
	_, err := fixture.service.Login(t.Context(), "test", "test")
	if err == nil || errors.Is(err, ErrAuthentication) {
		t.Fatalf("storage failure converted to wrong password: %v", err)
	}
}

func TestAuthenticationContextDoesNotHideStorageFailure(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	session := authenticatedTestAdmin(t, fixture)
	if _, err := fixture.database.SQL.ExecContext(t.Context(), `DROP TABLE auth_sessions`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Context(t.Context(), session.CookieToken); err == nil {
		t.Fatal("authentication context hid a storage failure")
	}
}
