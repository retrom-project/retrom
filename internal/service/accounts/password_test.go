package accounts

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/accounts"

	"retrom/internal/capability/security/authn"
)

type passwordMemory struct {
	state      model.PasswordState
	plan       model.PasswordPlan
	writeCalls int
	lateError  error
}

func (memory *passwordMemory) Current(context.Context, model.PasswordActor, int64) (model.PasswordState, bool, error) {
	return memory.state, true, nil
}

func (memory *passwordMemory) CommitChangePassword(
	_ context.Context, cmd model.ChangePasswordCommand,
) (model.PasswordChangeResult, error) {
	if !model.PasswordAuthorized(memory.state, cmd.Actor) ||
		memory.state.Credential.PasswordHash != cmd.ExpectedHash {
		return model.PasswordChangeResult{}, model.ErrAuthenticationNeeded
	}
	version := memory.state.Credential.SessionVersion + 1
	memory.plan = model.PasswordPlan{
		Actor:        cmd.Actor,
		ExpectedHash: cmd.ExpectedHash,
		NewHash:      cmd.NewHash,
		AuditID:      cmd.AuditID,
		Session:      cmd.Session,
		Now:          cmd.NowMS,
	}
	memory.writeCalls++
	if memory.lateError != nil {
		return model.PasswordChangeResult{}, memory.lateError
	}
	return model.PasswordChangeResult{
		User:      memory.state.Credential.User,
		ProfileID: memory.state.Credential.ProfileID,
		Version:   version,
	}, nil
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

func passwordFixture() (*passwordMemory, model.PasswordActor) {
	return &passwordMemory{state: model.PasswordState{SessionCurrent: true, Credential: model.LoginCredential{User: model.User{UserID: "user", Username: "alice", DisplayName: "Alice"}, Status: "ENABLED", SessionVersion: 2, PasswordHash: "old-hash"}}}, model.PasswordActor{UserID: "user", SessionID: "session", SessionVersion: 2}
}

func passwordMinter() (model.SessionMaterial, error) {
	return model.SessionMaterial{ID: "replacement"}, nil
}

func TestPasswordChangeRechecksCredentialAfterHashing(t *testing.T) {
	memory, actor := passwordFixture()
	hasher := passwordHasher{memory: memory, duringHash: func() { memory.state.Credential.PasswordHash = "concurrent-hash" }}
	service := NewPasswords(memory, hasher, authn.EmptyBlocklist{}, passwordMinter, time.Now)
	_, err := service.Change(t.Context(), actor, "old password", "replacement passphrase", "replacement passphrase")
	if !errors.Is(err, model.ErrAuthenticationNeeded) || memory.writeCalls != 0 {
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
