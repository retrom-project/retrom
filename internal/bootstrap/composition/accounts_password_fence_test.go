package composition

import (
	"errors"
	"testing"

	"retrom/internal/bootstrap/config"
	accountservice "retrom/internal/service/accounts"
)

func TestPasswordRotationRejectsRevokedPrincipalVersion(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	session := authenticatedTestAdmin(t, fixture)
	replacement := "first replacement passphrase"
	if _, err := fixture.service.ChangePassword(t.Context(), session.Principal, "test", replacement, replacement); err != nil {
		t.Fatal(err)
	}
	next := "second replacement passphrase"
	if _, err := fixture.service.ChangePassword(t.Context(), session.Principal, replacement, next, next); !errors.Is(err, accountservice.ErrAuthenticationNeeded) {
		t.Fatalf("revoked principal changed password: %v", err)
	}
}

func TestPasswordRotationRollsBackWhenDefaultCredentialFlagCannotBeSaved(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	session := authenticatedTestAdmin(t, fixture)
	if _, err := fixture.database.SQL.ExecContext(t.Context(), `DROP TABLE instance_state`); err != nil {
		t.Fatal(err)
	}
	replacement := "a strong replacement passphrase"
	if _, err := fixture.service.ChangePassword(t.Context(), session.Principal, "test", replacement, replacement); err == nil {
		t.Fatal("password change ignored failure to clear default credential flag")
	}
	if _, err := fixture.service.Login(t.Context(), "test", "test"); err != nil {
		t.Fatalf("failed password change was committed: %v", err)
	}
}
