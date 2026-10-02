package accounts

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

type deferredAuthWrite struct {
	*authMemory
	committedChange func()
	reads           int
}

func (repository *deferredAuthWrite) Session(ctx context.Context, digest [32]byte) (SessionSnapshot, bool, error) {
	repository.reads++
	return repository.authMemory.Session(ctx, digest)
}

func (repository *deferredAuthWrite) WithWrite(ctx context.Context, _ func(AuthScope) error) error {
	<-ctx.Done()
	if repository.committedChange != nil {
		repository.committedChange()
	}
	return ctx.Err()
}

func TestDeferredRenewalRechecksCommittedSecurityFacts(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*authMemory)
		want   error
		role   string
	}{
		{name: "valid committed expiry", change: func(*authMemory) {}},
		{name: "role changed", change: func(m *authMemory) { m.snapshot.User.Role = "USER" }, role: "USER"},
		{name: "revoked", change: func(m *authMemory) { now := int64(1); m.snapshot.RevokedAt = &now }, want: ErrAuthenticationNeeded},
		{name: "disabled", change: func(m *authMemory) { m.snapshot.Status = "DISABLED" }, want: ErrAuthenticationNeeded},
		{name: "password changed", change: func(m *authMemory) { m.snapshot.UserVersion++ }, want: ErrAuthenticationNeeded},
		{name: "expired", change: func(m *authMemory) { m.snapshot.IdleExpiry = 360000 }, want: ErrAuthenticationNeeded},
		{name: "removed", change: func(m *authMemory) { m.found = false }, want: ErrAuthenticationNeeded},
		{name: "reader failed", change: func(m *authMemory) { m.readError = context.Canceled }, want: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			memory := validAuthMemory()
			repository := &deferredAuthWrite{authMemory: memory, committedChange: func() { test.change(memory) }}
			service := NewAuthentication(repository, nil, nil, "", func() time.Time { return time.UnixMilli(360000) })
			token := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
			session, err := service.Authenticate(t.Context(), token)
			if !errors.Is(err, test.want) || repository.reads != 2 || memory.refreshed.ID != "" {
				t.Fatalf("deferred renewal session=%+v err=%v reads=%d", session, err, repository.reads)
			}
			if test.want == nil {
				if session.CookieToken != token || session.IdleExpiresAtMS != 400000 || session.Principal.Role != test.role {
					t.Fatal("deferred renewal changed committed session identity or expiry")
				}
			} else if session.CookieToken != "" {
				t.Fatal("invalid session survived deferred renewal")
			}
		})
	}
}

func TestDeferredRenewalPreservesRequestCancellation(t *testing.T) {
	repository := &deferredAuthWrite{authMemory: validAuthMemory()}
	service := NewAuthentication(repository, nil, nil, "", func() time.Time { return time.UnixMilli(360000) })
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	session, err := service.Authenticate(ctx, base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	if !errors.Is(err, context.DeadlineExceeded) || session.CookieToken != "" || repository.reads != 1 {
		t.Fatalf("request cancellation was ignored: %+v %v reads=%d", session, err, repository.reads)
	}
}
