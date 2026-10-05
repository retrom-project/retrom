package accounts

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/service/accounts"
	"retrom/internal/testsupport"
	"retrom/internal/testsupport/testpostgres"
)

func TestLoginRetriesSerializationAndCreatesOneSession(t *testing.T) {
	t.Parallel()
	database, err := testsupport.OpenDatabase(t.Context(), testpostgres.DSN(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	seedLoginRetryUser(t, database.SQL)
	var conflicted atomic.Bool
	faultDatabase := testsupport.OpenSQLFaultDatabase(t, database.SQL, testsupport.SQLFaultHooks{
		BeforeExec: func(ctx context.Context, query string, _ []driver.NamedValue) error {
			if !strings.Contains(query, "UPDATE users SET last_login_at_ms=") ||
				!conflicted.CompareAndSwap(false, true) {
				return nil
			}
			_, err := database.SQL.ExecContext(ctx,
				"UPDATE users SET updated_at_ms=updated_at_ms WHERE id='actor'")
			return err
		},
	})
	attempts := 0
	err = NewAuthentication(database.ReadOnly, faultDatabase).WithWrite(t.Context(), func(scope accounts.AuthScope) error {
		attempts++
		credential, found, err := scope.Read.Credential(t.Context(), "admin")
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("login credential disappeared on attempt %d", attempts)
		}
		return scope.Write.Login(t.Context(), credential, accounts.SessionRecord{
			ID: "session", UserID: "actor", SessionVersion: 1,
			CreatedAt: 100, LastSeen: 100, IdleExpiry: 200, AbsoluteExpiry: 300,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := dbapi.QueryRowContext(t.Context(), database.SQL,
		"SELECT count(*) FROM auth_sessions WHERE user_id='actor'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if !conflicted.Load() || attempts != 2 || count != 1 {
		t.Fatalf("conflicted=%v attempts=%d sessions=%d", conflicted.Load(), attempts, count)
	}
}

func seedLoginRetryUser(t *testing.T, database dbapi.DB) {
	t.Helper()
	_, err := database.ExecContext(t.Context(), `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('profile','Admin',1);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES('actor','profile','admin','Admin','ADMIN','ENABLED',1,1);
INSERT INTO user_credentials(user_id,password_hash,password_scheme,password_changed_at_ms,created_at_ms)
VALUES('actor','hash','ARGON2ID_V1',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
}
