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
	if err := work(AuthScope{Read: memory, Write: memory}); err != nil {
		return err
	}
	return memory.lateError
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
