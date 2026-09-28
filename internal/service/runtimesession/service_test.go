package runtimesession

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

type sessionMemory struct {
	snapshot Snapshot
	renewals int
}

func (m *sessionMemory) Find(_ context.Context, hash [32]byte) (Snapshot, bool, error) {
	return m.snapshot, hash == m.snapshot.Session.Hash, nil
}

func (m *sessionMemory) Ensure(_ context.Context, _ string, s Session, now int64) (Snapshot, error) {
	if m.snapshot.Session.ExpiresAtMS <= now {
		m.snapshot.Session = s
	}
	return m.snapshot, nil
}

func (m *sessionMemory) Renew(_ context.Context, _ string, now, expires int64) (Snapshot, error) {
	m.renewals++
	m.snapshot.Session.RenewedAtMS = now
	m.snapshot.Session.ExpiresAtMS = expires
	return m.snapshot, nil
}

func (*sessionMemory) Runs(context.Context, string, string, int64) ([]string, error) { return nil, nil }
func (*sessionMemory) ExtendRun(context.Context, string, string, int64, int64) error { return nil }

func newSessionFixture(t *testing.T) (*Service, *sessionMemory, *int64, Session) {
	t.Helper()
	now := int64(1000)
	memory := &sessionMemory{snapshot: Snapshot{Enabled: true, UserVersion: 1, SessionVersion: 1}}
	service := New(memory, Environment{
		Now:   func() time.Time { return time.UnixMilli(now) },
		NewID: func() (string, error) { return "session-id", nil }, Sign: func(id string) (string, error) {
			hash := sha256.Sum256([]byte(id))
			return base64.RawURLEncoding.EncodeToString(hash[:]), nil
		},
	})
	issued, err := service.Ensure(t.Context(), "auth-session")
	if err != nil {
		t.Fatal(err)
	}
	return service, memory, &now, issued
}

func TestRuntimeCredentialRenewsOnlyAfterHalfItsLifetime(t *testing.T) {
	t.Parallel()
	service, memory, now, issued := newSessionFixture(t)
	if issued.ExpiresAtMS != *now+LifetimeMS {
		t.Fatal("runtime credential must last 24 hours")
	}
	*now += RenewalAgeMS
	unchanged, err := service.Authenticate(t.Context(), issued.Token)
	if err != nil || unchanged.ExpiresAtMS != issued.ExpiresAtMS || memory.renewals != 0 {
		t.Fatal("renewed at or before 12 hours")
	}
	*now++
	renewed, err := service.Authenticate(t.Context(), issued.Token)
	if err != nil || renewed.Token != issued.Token || renewed.ExpiresAtMS != *now+LifetimeMS || !renewed.Refreshed || memory.renewals != 1 {
		t.Fatal("renewal must preserve the shared credential and extend it by 24 hours")
	}
	another, err := service.Authenticate(t.Context(), issued.Token)
	if err != nil || another.Token != issued.Token || another.ExpiresAtMS != renewed.ExpiresAtMS || memory.renewals != 1 {
		t.Fatal("another game request must reuse the renewed credential")
	}
}

func TestRuntimeCredentialCannotRenewAfterExpiryOrRevocation(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"expired", "disabled", "revoked", "version", "forged"} {
		t.Run(reason, func(t *testing.T) {
			service, memory, now, issued := newSessionFixture(t)
			*now += RenewalAgeMS + 1
			switch reason {
			case "expired":
				*now = issued.ExpiresAtMS
			case "disabled":
				memory.snapshot.Enabled = false
			case "revoked":
				memory.snapshot.Revoked = true
			case "version":
				memory.snapshot.UserVersion++
			case "forged":
				issued.Token = "invalid"
			}
			if _, err := service.Authenticate(t.Context(), issued.Token); !errors.Is(err, ErrCredential) || memory.renewals != 0 {
				t.Fatalf("%s credential was accepted or renewed", reason)
			}
		})
	}
}

func (*sessionMemory) FinishablePreview(context.Context, string, string, int64) (bool, error) {
	return false, nil
}
