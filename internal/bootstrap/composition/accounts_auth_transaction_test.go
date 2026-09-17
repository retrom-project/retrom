package composition

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	"retrom/internal/bootstrap/config"
	accountservice "retrom/internal/model/accounts"
	accountpersistence "retrom/internal/repo/accounts"
	"retrom/internal/testkit/testsupport"
)

func TestLoginSessionAndUserActivityRollbackTogether(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	session := authenticatedTestAdmin(t, fixture)
	var before int64
	if err := fixture.database.SQL.QueryRowContext(t.Context(), `SELECT last_login_at_ms FROM users WHERE id=?`, session.User.UserID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	credential, found, err := accountpersistence.NewAuthentication(fixture.database.SQL).Credential(t.Context(), session.User.Username)
	if err != nil || !found {
		t.Fatalf("credential missing: %v", err)
	}
	faultDB := testsupport.OpenSQLFaultDatabase(t, fixture.database.SQL, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, _ []driver.NamedValue) error {
			if strings.Contains(query, "INSERT INTO auth_sessions") {
				return context.Canceled
			}
			return nil
		},
	})
	repository := accountpersistence.NewAuthentication(faultDB)
	material := accountservice.SessionMaterial{ID: "rollback-session", Hash: [32]byte{1}}
	err = repository.CommitLogin(t.Context(), accountservice.LoginCommand{
		Credential: credential,
		Session:    material.Record(session.User.UserID, credential.SessionVersion, before+100),
	})
	if err == nil {
		t.Fatal("expected session insert fault to cause error")
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
