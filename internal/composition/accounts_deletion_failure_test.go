package composition

import (
	"testing"

	"retrom/internal/config"
)

func TestUserDeletionRollsBackWhenDefaultCredentialFlagFails(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	initial := authenticatedTestAdmin(t, fixture)
	operator := acceptFixtureInvitation(t, fixture, initial.Principal, "ADMIN", "operator", "Operator")
	if _, err := fixture.database.SQL.ExecContext(t.Context(), `DROP TABLE instance_state`); err != nil {
		t.Fatal(err)
	}
	_, err := fixture.service.DeleteUser(t.Context(), operator.Principal, initial.User.UserID, 1, "test", "delete-default")
	if err == nil {
		t.Fatal("user deletion committed despite failing default credential cleanup")
	}
	if _, err := fixture.service.Authenticate(t.Context(), initial.CookieToken); err != nil {
		t.Fatalf("failed deletion revoked user session: %v", err)
	}
	var status string
	if err := fixture.database.SQL.QueryRowContext(t.Context(), `SELECT status FROM users WHERE id=?`, initial.User.UserID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "ENABLED" {
		t.Fatalf("failed deletion changed user state: %s", status)
	}
}
