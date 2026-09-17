package accounts

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/accounts"
)

type authMemory struct {
	credential           model.LoginCredential
	snapshot             model.SessionSnapshot
	refreshed            model.SessionRefresh
	committed            model.SessionRecord
	found                bool
	readError, lateError error
	writeCalls           int
	duringWrite          func()
}

func (memory *authMemory) Credential(context.Context, string) (model.LoginCredential, bool, error) {
	return memory.credential, memory.found, memory.readError
}

func (memory *authMemory) Session(context.Context, [32]byte) (model.SessionSnapshot, bool, error) {
	return memory.snapshot, memory.found, memory.readError
}

func (memory *authMemory) CommitLogin(_ context.Context, cmd model.LoginCommand) error {
	memory.writeCalls++
	if memory.duringWrite != nil {
		memory.duringWrite()
	}
	memory.committed = cmd.Session
	return memory.lateError
}

func (memory *authMemory) CommitLogout(context.Context, model.LogoutCommand) error {
	memory.writeCalls++
	return memory.lateError
}

func (memory *authMemory) CommitRefreshSession(
	_ context.Context, cmd model.RefreshSessionCommand,
) (model.SessionSnapshot, error) {
	memory.writeCalls++
	if memory.duringWrite != nil {
		memory.duringWrite()
	}
	if !memory.found || !model.ValidSession(memory.snapshot, cmd.NowMS) {
		return model.SessionSnapshot{}, model.ErrAuthenticationNeeded
	}
	if cmd.NowMS-memory.snapshot.LastSeen >= model.RefreshInterval.Milliseconds() {
		expiry := min(
			cmd.NowMS+model.IdleDuration.Milliseconds(),
			memory.snapshot.AbsoluteExpiry,
		)
		memory.refreshed = model.SessionRefresh{
			ID:               memory.snapshot.ID,
			ExpectedLastSeen: memory.snapshot.LastSeen,
			LastSeen:         cmd.NowMS,
			IdleExpiry:       expiry,
		}
		memory.snapshot.LastSeen = cmd.NowMS
		memory.snapshot.IdleExpiry = expiry
	}
	if memory.lateError != nil {
		return model.SessionSnapshot{}, memory.lateError
	}
	return memory.snapshot, nil
}

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
	if !errors.Is(err, context.Canceled) || errors.Is(err, model.ErrAuthentication) || verifier.calls != 1 || verifier.encoded != "dummy" || memory.writeCalls != 0 {
		t.Fatalf("credential failure: %v verifier=%+v", err, verifier)
	}
}

func TestRefreshRechecksRevocationBeforeExtendingSession(t *testing.T) {
	memory := validAuthMemory()
	memory.duringWrite = func() { now := int64(100); memory.snapshot.RevokedAt = &now }
	service := NewAuthentication(memory, nil, nil, "", func() time.Time { return time.UnixMilli(360000) })
	_, err := service.Authenticate(t.Context(), base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	if !errors.Is(err, model.ErrAuthenticationNeeded) || memory.refreshed.ID != "" {
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
	return &authMemory{found: true, snapshot: model.SessionSnapshot{ID: "session", Status: "ENABLED", UserVersion: 1, SessionVersion: 1, LastSeen: 0, IdleExpiry: 400000, AbsoluteExpiry: 400000}}
}
