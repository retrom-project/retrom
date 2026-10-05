//go:build integration

package httpapi

import (
	"crypto/sha256"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

func testOtherRuntimeCookie(t *testing.T, server *testServer) *http.Cookie {
	t.Helper()
	const authID = "01980000-0000-7000-8000-000000009996"
	_, err := server.database.ExecContext(t.Context(), `INSERT INTO profiles(id,display_name,created_at_ms)
 VALUES('other-runtime-profile','Other',0) ON CONFLICT DO NOTHING;
 INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
 VALUES('01980000-0000-7000-8000-000000009997','other-runtime-profile','other-runtime-user','Other','USER','ENABLED',0,0) ON CONFLICT DO NOTHING;`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = server.database.ExecContext(t.Context(), `INSERT INTO auth_sessions(id,user_id,token_sha256,user_session_version,
 created_at_ms,last_seen_at_ms,idle_expires_at_ms,absolute_expires_at_ms) VALUES(?,?,?,1,0,0,?,?) ON CONFLICT DO NOTHING`, authID,
		"01980000-0000-7000-8000-000000009997", makeOtherSessionHash(), time.Now().Add(24*time.Hour).UnixMilli(), time.Now().Add(24*time.Hour).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	session, err := server.playDeps.RuntimeSessions.Ensure(t.Context(), authID)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: runtimeCookieName, Value: session.Token, Path: "/"}
}

func makeOtherSessionHash() []byte { value := make([]byte, 32); value[0] = 1; return value }

func testRuntimeCookieForProfile(t *testing.T, server *testServer, profile string) *http.Cookie {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	hash := sha256.Sum256([]byte(id))
	expires := time.Now().Add(24 * time.Hour).UnixMilli()
	_, err := server.database.ExecContext(t.Context(), `INSERT INTO auth_sessions(id,user_id,token_sha256,user_session_version,
 created_at_ms,last_seen_at_ms,idle_expires_at_ms,absolute_expires_at_ms)
 SELECT ?,id,?,session_version,0,0,?,? FROM users WHERE profile_id=?`, id, hash[:], expires, expires, profile)
	if err != nil {
		t.Fatal(err)
	}
	session, err := server.playDeps.RuntimeSessions.Ensure(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: runtimeCookieName, Value: session.Token, Path: "/"}
}
