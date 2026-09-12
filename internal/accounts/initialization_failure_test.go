package accounts

import (
	"errors"
	"testing"

	"retrom/internal/authn"
	"retrom/internal/config"
)

func TestStartRejectsUserWithoutCredential(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	if err := fixture.service.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.SQL.ExecContext(t.Context(), `DELETE FROM user_credentials`); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.Start(t.Context()); !errors.Is(err, authn.ErrCredential) {
		t.Fatalf("started instance with missing credential: %v", err)
	}
}
