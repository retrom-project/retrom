package accounts

import (
	"context"
	"errors"
	"testing"
	"time"

	"retrom/internal/capability/security/authn"
)

type passwordMemory struct {
	state      PasswordState
	plan       PasswordPlan
	writeCalls int
	lateError  error
}

func (memory *passwordMemory) Current(context.Context, PasswordActor, int64) (PasswordState, bool, error) {
	return memory.state, true, nil
}

func (memory *passwordMemory) WithWrite(_ context.Context, work func(PasswordScope) error) error {
	if err := work(PasswordScope{Read: memory, Write: memory}); err != nil {
		return err
	}
	return memory.lateError
}

func (memory *passwordMemory) Rotate(_ context.Context, plan PasswordPlan) error {
	memory.plan = plan
	memory.writeCalls++
	return nil
}

type passwordHasher struct {
	memory     *passwordMemory
	duringHash func()
}

func (hasher passwordHasher) Verify(context.Context, string, string) (bool, error) { return true, nil }

func (hasher passwordHasher) Hash(context.Context, string) (string, error) {
	if hasher.duringHash != nil {
		hasher.duringHash()
	}
	return "new-hash", nil
}

func passwordFixture() (*passwordMemory, PasswordActor) {
	return &passwordMemory{state: PasswordState{SessionCurrent: true, Credential: LoginCredential{User: User{UserID: "user", Username: "alice", DisplayName: "Alice"}, Status: "ENABLED", SessionVersion: 2, PasswordHash: "old-hash"}}}, PasswordActor{UserID: "user", SessionID: "session", SessionVersion: 2}
}
func passwordMinter() (SessionMaterial, error) { return SessionMaterial{ID: "replacement"}, nil }
func TestPasswordChangeRechecksCredentialAfterHashing(t *testing.T) {
	memory, actor := passwordFixture()
	hasher := passwordHasher{memory: memory, duringHash: func() { memory.state.Credential.PasswordHash = "concurrent-hash" }}
	service := NewPasswords(memory, hasher, authn.EmptyBlocklist{}, passwordMinter, time.Now)
	_, err := service.Change(t.Context(), actor, "old password", "replacement passphrase", "replacement passphrase")
	if !errors.Is(err, ErrAuthenticationNeeded) || memory.writeCalls != 0 {
		t.Fatalf("overwrote concurrent credential: %v", err)
	}
}

func TestPasswordCommitFailureReturnsNoReplacementSession(t *testing.T) {
	memory, actor := passwordFixture()
	memory.lateError = context.Canceled
	service := NewPasswords(memory, passwordHasher{memory: memory}, authn.EmptyBlocklist{}, passwordMinter, func() time.Time { return time.UnixMilli(100) })
	session, err := service.Change(t.Context(), actor, "old password", "replacement passphrase", "replacement passphrase")
	if !errors.Is(err, context.Canceled) || session.Principal.SessionID != "" || memory.writeCalls != 1 || memory.plan.Session.SessionVersion != 3 || memory.plan.ExpectedHash != "old-hash" {
		t.Fatalf("password commit: %+v / %v", memory.plan, err)
	}
}
