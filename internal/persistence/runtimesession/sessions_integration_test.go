//go:build integration

package runtimesession

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/service/runtimesession"
	"retrom/internal/store"

	"github.com/google/uuid"
)

func sessionFixture(t *testing.T) (*runtimesession.Service, dbapi.DB, *int64, runtimesession.Session) {
	t.Helper()
	now := int64(1000)
	clock := func() time.Time { return time.UnixMilli(now) }
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "runtime.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	_, err = db.SQL.ExecContext(t.Context(), `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('runtime-owner','Owner',0);
 INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
 VALUES('runtime-user','runtime-owner','runtime-user','Owner','USER','ENABLED',0,0);
 INSERT INTO auth_sessions(id,user_id,token_sha256,user_session_version,created_at_ms,last_seen_at_ms,idle_expires_at_ms,absolute_expires_at_ms)
 VALUES('auth-runtime','runtime-user',zeroblob(32),1,0,0,28800000,86400000);`)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := runtimesession.New(New(db.SQL), runtimesession.Environment{
		Now: clock, Sign: credentials.RuntimeSession,
		NewID: func() (string, error) { id, err := uuid.NewV7(); return id.String(), err },
	})
	issued, err := service.Ensure(t.Context(), "auth-runtime")
	if err != nil {
		t.Fatal(err)
	}
	return service, db.SQL, &now, issued
}

func TestSharedRuntimeSurvivesLoginExpiryAndConcurrentRenewal(t *testing.T) {
	t.Parallel()
	service, db, now, issued := sessionFixture(t)
	for range 50 {
		reused, err := service.Ensure(t.Context(), "auth-runtime")
		if err != nil || reused.Token != issued.Token {
			t.Fatal("runtime issuance did not reuse its login session credential")
		}
	}
	*now += runtimesession.RenewalAgeMS + 1
	var wait sync.WaitGroup
	for range 8 {
		wait.Go(func() {
			refreshed, err := service.Authenticate(context.Background(), issued.Token)
			if err != nil || refreshed.Token != issued.Token || refreshed.ExpiresAtMS != *now+runtimesession.LifetimeMS {
				t.Error("concurrent runtime renewal changed the credential or lost its expiry")
			}
		})
	}
	wait.Wait()
	*now += runtimesession.RenewalAgeMS + 1
	afterLoginExpiry, err := service.Authenticate(t.Context(), issued.Token)
	if err != nil || afterLoginExpiry.ExpiresAtMS != *now+runtimesession.LifetimeMS {
		t.Fatal("login natural expiry interrupted runtime renewal")
	}
	_, err = db.ExecContext(t.Context(), `UPDATE auth_sessions SET revoked_at_ms=?,revoked_reason='LOGOUT' WHERE id='auth-runtime'`, *now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Authenticate(t.Context(), issued.Token); !errors.Is(err, runtimesession.ErrCredential) {
		t.Fatal("logout did not revoke the shared runtime credential")
	}
}

func TestExpiredRuntimeRequiresFreshAccountAuthorization(t *testing.T) {
	t.Parallel()
	service, _, now, issued := sessionFixture(t)
	*now = issued.ExpiresAtMS
	if _, err := service.Authenticate(t.Context(), issued.Token); !errors.Is(err, runtimesession.ErrCredential) {
		t.Fatal("expired token renewed itself")
	}
	if _, err := service.Ensure(t.Context(), "auth-runtime"); !errors.Is(err, runtimesession.ErrCredential) {
		t.Fatal("expired login minted a new runtime credential")
	}
}
