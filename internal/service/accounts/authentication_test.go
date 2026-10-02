package accounts

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

type authMemory struct {
	credential           LoginCredential
	snapshot             SessionSnapshot
	refreshed            SessionRefresh
	committed            SessionRecord
	found                bool
	readError, lateError error
	writeCalls           int
	duringWrite          func()
	limits               *limitMemory
}

func (memory *authMemory) Credential(context.Context, string) (LoginCredential, bool, error) {
	return memory.credential, memory.found, memory.readError
}

func (memory *authMemory) Session(context.Context, [32]byte) (SessionSnapshot, bool, error) {
	return memory.snapshot, memory.found, memory.readError
}

func (memory *authMemory) WithWrite(_ context.Context, work func(AuthScope) error) error {
	memory.writeCalls++
	if memory.duringWrite != nil {
		memory.duringWrite()
	}
	if err := work(AuthScope{Read: memory, Write: memory, Limits: memory.limits}); err != nil {
		return err
	}
	return memory.lateError
}

func TestLoginRechecksLimitsAfterWaitingForWriter(t *testing.T) {
	key := RateLimitKey{Scope: "LOGIN_ACCOUNT"}
	limits := &limitMemory{values: map[RateLimitKey]RateLimitBucket{}}
	memory := &authMemory{found: true, credential: LoginCredential{Status: "ENABLED"}, limits: limits}
	memory.duringWrite = func() {
		until := int64(200)
		limits.values[key] = RateLimitBucket{Key: key, BlockedUntil: &until}
	}
	service := NewAuthentication(memory, &authVerifier{}, func() (SessionMaterial, error) {
		return SessionMaterial{ID: "session"}, nil
	}, "dummy", func() time.Time { return time.UnixMilli(100) })
	session, err := service.login(t.Context(), "alice", "password", []RateLimitKey{key}, &loginTiming{})
	if !errors.Is(err, ErrRateLimited) || memory.committed.ID != "" || session.CookieToken != "" || limits.cleared.Scope != "" {
		t.Fatalf("concurrent block bypassed: session=%+v committed=%+v cleared=%+v err=%v", session, memory.committed, limits.cleared, err)
	}
}

func TestSuccessfulLoginClearsOnlyAccountLimitInsideSessionWrite(t *testing.T) {
	limits := &limitMemory{values: map[RateLimitKey]RateLimitBucket{}}
	memory := &authMemory{found: true, credential: LoginCredential{Status: "ENABLED"}, limits: limits}
	now := func() time.Time { return time.UnixMilli(100) }
	authentication := NewAuthentication(memory, &authVerifier{}, func() (SessionMaterial, error) {
		return SessionMaterial{ID: "session", Token: base64.RawURLEncoding.EncodeToString(make([]byte, 32))}, nil
	}, "dummy", now)
	service := &Service{modules: Modules{
		Authentication: authentication, Limiter: NewLimiter(limits, limitHasher{}, now),
	}}
	if _, err := service.LoginRateLimited(t.Context(), "alice", "password", "192.0.2.1"); err != nil {
		t.Fatal(err)
	}
	if memory.writeCalls != 1 || memory.committed.ID != "session" ||
		limits.cleared.Scope != "LOGIN_ACCOUNT" ||
		limits.cleared.Digest != (limitHasher{}).RateLimitSubject("LOGIN_ACCOUNT", "alice") {
		t.Fatalf("login scopes: writes=%d session=%s clear=%+v", memory.writeCalls, memory.committed.ID, limits.cleared)
	}
}

func (memory *authMemory) Login(_ context.Context, _ LoginCredential, value SessionRecord) error {
	memory.committed = value
	return nil
}

func (memory *authMemory) Refresh(_ context.Context, value SessionRefresh) error {
	memory.refreshed = value
	return nil
}
func (memory *authMemory) Revoke(context.Context, string, int64) error { return nil }

type authVerifier struct {
	encoded string
	calls   int
}

func (verifier *authVerifier) Verify(_ context.Context, _ string, encoded string) (bool, error) {
	verifier.encoded = encoded
	verifier.calls++
	return true, nil
}

func TestCredentialFailureStillVerifiesDummyAndPreservesStorageCause(t *testing.T) {
	memory := &authMemory{readError: context.Canceled}
	verifier := &authVerifier{}
	service := NewAuthentication(memory, verifier, nil, "dummy", time.Now)
	_, err := service.Login(t.Context(), "alice", "password")
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrAuthentication) || verifier.calls != 1 || verifier.encoded != "dummy" || memory.writeCalls != 0 {
		t.Fatalf("credential failure: %v verifier=%+v", err, verifier)
	}
}

func TestRefreshRechecksRevocationBeforeExtendingSession(t *testing.T) {
	memory := validAuthMemory()
	memory.duringWrite = func() { now := int64(100); memory.snapshot.RevokedAt = &now }
	service := NewAuthentication(memory, nil, nil, "", func() time.Time { return time.UnixMilli(360000) })
	_, err := service.Authenticate(t.Context(), base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	if !errors.Is(err, ErrAuthenticationNeeded) || memory.refreshed.ID != "" {
		t.Fatalf("revoked session extended: %+v %v", memory.refreshed, err)
	}
}

func TestRefreshCommitFailureReturnsNoSession(t *testing.T) {
	memory := validAuthMemory()
	memory.lateError = context.Canceled
	service := NewAuthentication(memory, nil, nil, "", func() time.Time { return time.UnixMilli(360000) })
	session, err := service.Authenticate(t.Context(), base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	if !errors.Is(err, context.Canceled) || session.CookieToken != "" || memory.refreshed.ID != "session" || memory.refreshed.IdleExpiry != 400000 {
		t.Fatalf("refresh commit: %+v / %+v / %v", session, memory.refreshed, err)
	}
}

func validAuthMemory() *authMemory {
	return &authMemory{found: true, snapshot: SessionSnapshot{ID: "session", Status: "ENABLED", UserVersion: 1, SessionVersion: 1, LastSeen: 0, IdleExpiry: 400000, AbsoluteExpiry: 400000}}
}
