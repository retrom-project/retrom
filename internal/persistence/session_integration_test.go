//go:build integration

package persistence_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/testsupport"
)

func TestSessionSlidingExpiryIsCappedAtSevenDays(t *testing.T) {
	t.Parallel()
	f := testsupport.Library(t)
	now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	id := uuid.NewString()
	const token = "session-lifetime-test"
	if err := f.Repository.CreateSession(t.Context(), id, f.Principal.User.ID, token, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	absolute := now.Add(7 * 24 * time.Hour).UnixMilli()
	assertSessionExpiry(t, f.Repository, id, now.UnixMilli(), now.Add(3*24*time.Hour).UnixMilli(), absolute)
	for _, step := range []struct {
		after time.Duration
		idle  time.Duration
	}{
		{2 * 24 * time.Hour, 5 * 24 * time.Hour},
		{4 * 24 * time.Hour, 7 * 24 * time.Hour},
		{6 * 24 * time.Hour, 7 * 24 * time.Hour},
		{7*24*time.Hour - time.Millisecond, 7 * 24 * time.Hour},
	} {
		at := now.Add(step.after).UnixMilli()
		principal, err := f.Repository.Authenticate(t.Context(), token, at)
		if err != nil || principal.User.ID != f.Principal.User.ID || principal.SessionID != id {
			t.Fatalf("authenticate after %s: principal=%+v error=%v", step.after, principal, err)
		}
		assertSessionExpiry(t, f.Repository, id, at, now.Add(step.idle).UnixMilli(), absolute)
	}
	if _, err := f.Repository.Authenticate(t.Context(), token, absolute); !errors.Is(err, model.ErrUnauthorized) {
		t.Fatalf("absolute expiry error=%v", err)
	}
	assertSessionExpiry(t, f.Repository, id, absolute-1, absolute, absolute)
}

func TestSessionExpiresAfterThreeDaysWithoutActivity(t *testing.T) {
	t.Parallel()
	for _, item := range []struct {
		name    string
		after   time.Duration
		allowed bool
	}{
		{"past former eight hour limit", 12 * time.Hour, true},
		{"just before idle expiry", 3*24*time.Hour - time.Millisecond, true},
		{"exact idle expiry", 3 * 24 * time.Hour, false},
		{"past idle expiry", 3*24*time.Hour + time.Millisecond, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.Library(t)
			const now = int64(1000)
			id := uuid.NewString()
			const token = "idle-lifetime-test"
			if err := f.Repository.CreateSession(t.Context(), id, f.Principal.User.ID, token, now); err != nil {
				t.Fatal(err)
			}
			_, err := f.Repository.Authenticate(t.Context(), token, now+item.after.Milliseconds())
			if item.allowed {
				if err != nil {
					t.Fatalf("authenticate after %s: %v", item.after, err)
				}
			} else {
				if !errors.Is(err, model.ErrUnauthorized) {
					t.Fatalf("expired session error=%v", err)
				}
				assertSessionExpiry(t, f.Repository, id, now,
					now+(3*24*time.Hour).Milliseconds(), now+(7*24*time.Hour).Milliseconds())
			}
		})
	}
}

func assertSessionExpiry(t *testing.T, r *persistence.Repository, id string, seen, idle, absolute int64) {
	t.Helper()
	count, err := r.Count(t.Context(), `SELECT count(*) FROM auth_session_tab
 WHERE id=$1 AND last_seen_at_ms=$2 AND idle_expires_at_ms=$3 AND absolute_expires_at_ms=$4`,
		id, seen, idle, absolute)
	if err != nil || count != 1 {
		t.Fatalf("session expiry mismatch: seen=%d idle=%d absolute=%d matches=%d error=%v", seen, idle, absolute, count, err)
	}
}
