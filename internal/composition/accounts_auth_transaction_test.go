package composition

import (
	"context"
	"errors"
	"testing"

	"retrom/internal/config"
	dbapi "retrom/internal/database"
	accountpersistence "retrom/internal/persistence/accounts"
	accountservice "retrom/internal/service/accounts"
)

func TestLoginSessionAndUserActivityRollbackTogether(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	session := authenticatedTestAdmin(t, fixture)
	var before int64
	if err := dbapi.QueryRowContext(t.Context(), fixture.database.SQL, `SELECT last_login_at_ms FROM users WHERE id=?`, session.User.UserID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	repository := accountpersistence.NewAuthentication(fixture.database.ReadOnly, fixture.database.SQL)
	credential, found, err := repository.Credential(t.Context(), session.User.Username)
	if err != nil || !found {
		t.Fatalf("credential missing: %v", err)
	}
	material := accountservice.SessionMaterial{ID: "rollback-session", Hash: [32]byte{1}}
	key := accountservice.RateLimitKey{Scope: "LOGIN_ACCOUNT", Digest: [32]byte{2}}
	if _, err := fixture.database.SQL.ExecContext(t.Context(), `INSERT INTO auth_rate_limits
 (scope,subject_hash,window_started_at_ms,failure_count,updated_at_ms) VALUES(?,?,1,2,1)`,
		key.Scope, key.Digest[:]); err != nil {
		t.Fatal(err)
	}
	err = repository.WithWrite(t.Context(), func(scope accountservice.AuthScope) error {
		if err := scope.Write.Login(t.Context(), credential, material.Record(session.User.UserID, credential.SessionVersion, before+100)); err != nil {
			return err
		}
		if err := scope.Limits.Clear(t.Context(), key); err != nil {
			return err
		}
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("late login write: %v", err)
	}
	var after int64
	var sessions int
	if err := dbapi.QueryRowContext(t.Context(), fixture.database.SQL, `SELECT last_login_at_ms,(SELECT count(*) FROM auth_sessions WHERE id='rollback-session') FROM users WHERE id=?`, session.User.UserID).Scan(&after, &sessions); err != nil {
		t.Fatal(err)
	}
	if after != before || sessions != 0 {
		t.Fatalf("partial login: lastSeen=%d/%d sessions=%d", before, after, sessions)
	}
	limit, found, err := accountpersistence.NewRateLimits(fixture.database.ReadOnly, fixture.database.SQL).Read(t.Context(), key)
	if err != nil || !found || limit.Failures != 2 {
		t.Fatalf("login rollback lost failure bucket: %+v %t %v", limit, found, err)
	}
}
