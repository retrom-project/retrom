package composition

import (
	"context"
	"errors"
	"testing"

	"retrom/internal/bootstrap/config"
	accountsmodel "retrom/internal/model/accounts"
	accountpersistence "retrom/internal/repo/accounts"
)

func TestLoginSessionAndUserActivityRollbackTogether(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	session := authenticatedTestAdmin(t, fixture)
	var before int64
	if err := fixture.database.SQL.QueryRowContext(t.Context(), `SELECT last_login_at_ms FROM users WHERE id=?`, session.User.UserID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	repository := accountpersistence.NewAuthentication(fixture.database.SQL)
	credential, found, err := repository.Credential(t.Context(), session.User.Username)
	if err != nil || !found {
		t.Fatalf("credential missing: %v", err)
	}
	material := accountsmodel.SessionMaterial{ID: "rollback-session", Hash: [32]byte{1}}
	err = repository.WithWrite(t.Context(), func(scope accountsmodel.AuthScope) error {
		if err := scope.Write.Login(t.Context(), credential, material.Record(session.User.UserID, credential.SessionVersion, before+100)); err != nil {
			return err
		}
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("late login write: %v", err)
	}
	var after int64
	var sessions int
	if err := fixture.database.SQL.QueryRowContext(t.Context(), `SELECT last_login_at_ms,(SELECT count(*) FROM auth_sessions WHERE id='rollback-session') FROM users WHERE id=?`, session.User.UserID).Scan(&after, &sessions); err != nil {
		t.Fatal(err)
	}
	if after != before || sessions != 0 {
		t.Fatalf("partial login: lastSeen=%d/%d sessions=%d", before, after, sessions)
	}
}
