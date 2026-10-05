package composition

import (
	"context"
	"testing"
	"time"

	"retrom/internal/config"
	dbapi "retrom/internal/database"
)

func TestAuthenticationReadsCommittedFactsWhileWriterIsHeld(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	session := authenticatedTestAdmin(t, fixture)
	tx, err := fixture.database.SQL.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbapi.Rollback(tx)
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	current, err := fixture.service.Authenticate(ctx, session.CookieToken)
	if err != nil {
		t.Fatalf("authentication queued behind writer: %v", err)
	}
	if current.Principal.UserID != session.Principal.UserID {
		t.Fatal("wrong authenticated principal")
	}
}

func TestAuthenticationDueForRenewalDoesNotWaitForBackgroundWriter(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	session := authenticatedTestAdmin(t, fixture)
	*fixture.now = fixture.now.Add(6 * time.Minute)
	tx, err := fixture.database.SQL.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbapi.Rollback(tx)
	// Hold the same row: unrelated PostgreSQL writes do not block renewal.
	if _, err := tx.ExecContext(t.Context(), "UPDATE auth_sessions SET last_seen_at_ms=last_seen_at_ms WHERE id=?", session.Principal.SessionID); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	current, err := fixture.service.Authenticate(ctx, session.CookieToken)
	if err != nil || current.Principal.UserID != session.Principal.UserID {
		t.Fatalf("optional session renewal blocked authentication: %v", err)
	}
	if current.IdleExpiresAtMS != session.IdleExpiresAtMS {
		t.Fatal("uncommitted renewal extended expiry")
	}
}
