package accounts

import (
	"context"
	"errors"
	model "retrom/internal/model/accounts"
	"testing"
	"time"
)

type consumptionMemory struct {
	state                           model.ResetState
	present, exists, inWrite        bool
	transactions, snapshots, writes int
	readErr, lateErr                error
	invitation                      model.InvitationAcceptance
	reset                           model.ResetConsumption
	audits                          []model.AccountAudit
}

func (memory *consumptionMemory) ResetState(context.Context, string) (model.ResetState, bool, error) {
	memory.snapshots++
	return memory.state, memory.present, memory.readErr
}

func (memory *consumptionMemory) WithConsumptionWrite(_ context.Context, work func(model.LinkConsumptionScope) error) error {
	memory.transactions++
	memory.inWrite = true
	defer func() { memory.inWrite = false }()
	if err := work(model.LinkConsumptionScope{Read: memory, Write: memory}); err != nil {
		return err
	}
	return memory.lateErr
}

func (memory *consumptionMemory) Current(context.Context, string) (model.LinkRecord, bool, error) {
	return memory.state.Link, memory.present, memory.readErr
}

func (memory *consumptionMemory) Target(context.Context, string) (model.LinkTarget, bool, error) {
	return memory.state.Target, memory.present, memory.readErr
}

func (memory *consumptionMemory) UsernameExists(context.Context, string) (bool, error) {
	return memory.exists, memory.readErr
}

func (memory *consumptionMemory) Accept(_ context.Context, plan model.InvitationAcceptance) error {
	memory.writes++
	memory.invitation = plan
	return nil
}

func (memory *consumptionMemory) Reset(_ context.Context, plan model.ResetConsumption) error {
	memory.writes++
	memory.reset = plan
	return nil
}

func (memory *consumptionMemory) Audit(_ context.Context, audit model.AccountAudit) error {
	memory.audits = append(memory.audits, audit)
	return nil
}

type consumptionHasher struct {
	onHash  func()
	calls   int
	inWrite *bool
}

func (hasher *consumptionHasher) Hash(context.Context, string) (string, error) {
	if *hasher.inWrite {
		return "", errors.New("hash ran inside write scope")
	}
	hasher.calls++
	if hasher.onHash != nil {
		hasher.onHash()
	}
	return "prepared-hash", nil
}

func (*consumptionHasher) Verify(context.Context, string, string) (bool, error) {
	return false, errors.New("unexpected Verify")
}

func consumptionFixture() (*LinkConsumptionService, *consumptionMemory, *consumptionHasher) {
	target := "target"
	role := "USER"
	memory := &consumptionMemory{present: true, state: model.ResetState{
		Link:   model.LinkRecord{Link: model.AccountLink{AccountLinkID: "link", Kind: "PASSWORD_RESET", Role: &role, TargetUserID: &target, Version: 2, ExpiresAtMS: 200}},
		Target: model.LinkTarget{User: model.User{UserID: target, Username: "alice", DisplayName: "Alice", Role: "USER"}, ProfileID: "profile", Status: "ENABLED", Version: 3, SessionVersion: 4},
	}}
	hasher := &consumptionHasher{inWrite: &memory.inWrite}
	service := NewLinkConsumption(memory, model.LinkConsumptionOptions{Tokens: linkTokens{true}, Hasher: hasher, Now: func() time.Time { return time.UnixMilli(100) }, Mint: func() (model.SessionMaterial, error) {
		return model.SessionMaterial{ID: "session", Token: "token"}, nil
	}})
	return service, memory, hasher
}

const consumptionPassword = "q8!brilliant violet rivers"

func resetConsumptionRequest() model.CompletePasswordResetRequest {
	return model.CompletePasswordResetRequest{Token: "capability", Password: consumptionPassword, PasswordConfirmation: consumptionPassword}
}

func invitationConsumptionRequest() model.AcceptInvitationRequest {
	return model.AcceptInvitationRequest{Token: "capability", Username: "bob", DisplayName: " Bob ", Password: consumptionPassword, PasswordConfirmation: consumptionPassword}
}

func TestResetConsumptionRechecksConcurrentChangesAfterHash(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*consumptionMemory)
	}{
		{"revoked", func(memory *consumptionMemory) { stamp := int64(100); memory.state.Link.Link.RevokedAtMS = &stamp }},
		{"expired", func(memory *consumptionMemory) { memory.state.Link.Link.ExpiresAtMS = 100 }},
		{"link version", func(memory *consumptionMemory) { memory.state.Link.Link.Version++ }},
		{"target version", func(memory *consumptionMemory) { memory.state.Target.Version++ }},
		{"target session version", func(memory *consumptionMemory) { memory.state.Target.SessionVersion++ }},
		{"target status", func(memory *consumptionMemory) { memory.state.Target.Status = "DISABLED" }},
		{"target deleted", func(memory *consumptionMemory) { memory.state.Target.Status = "DELETED" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, memory, hasher := consumptionFixture()
			hasher.onHash = func() { test.change(memory) }
			result, err := service.CompleteReset(t.Context(), resetConsumptionRequest())
			if !errors.Is(err, model.ErrAccountLinkUnavailable) || result.Session != nil || memory.writes != 0 || hasher.calls != 1 {
				t.Fatalf("concurrent reset: %+v %v writes=%d", result, err, memory.writes)
			}
		})
	}
}

func TestResetConsumptionDisabledAccountGetsNoSession(t *testing.T) {
	service, memory, _ := consumptionFixture()
	memory.state.Target.Status = "DISABLED"
	service.options.Mint = func() (model.SessionMaterial, error) {
		t.Fatal("disabled reset minted session")
		return model.SessionMaterial{}, nil
	}
	result, err := service.CompleteReset(t.Context(), resetConsumptionRequest())
	if err != nil || result.Session != nil || result.Status != "PASSWORD_CHANGED_ACCOUNT_DISABLED" || memory.reset.Session != nil || memory.writes != 1 {
		t.Fatalf("disabled reset: %+v %v", result, err)
	}
}

func TestResetConsumptionEnabledAccountRotatesSessionAfterHash(t *testing.T) {
	service, memory, hasher := consumptionFixture()
	memory.state.Target.User.Username = "test"
	result, err := service.CompleteReset(t.Context(), resetConsumptionRequest())
	if err != nil {
		t.Fatal(err)
	}
	if result.Session == nil || result.Session.Principal.SessionVersion != 5 || memory.reset.Session == nil || memory.reset.Session.SessionVersion != 5 || !memory.reset.ClearTestDefault || hasher.calls != 1 {
		t.Fatalf("session rotation: %+v %+v", result, memory.reset)
	}
	if len(memory.audits) != 1 || memory.audits[0].Action != "PASSWORD_RESET_COMPLETED" {
		t.Fatal("missing password reset audit")
	}
}

func TestConsumptionLateFailureDoesNotExposeSession(t *testing.T) {
	t.Run("reset", func(t *testing.T) {
		service, memory, _ := consumptionFixture()
		memory.lateErr = context.Canceled
		result, err := service.CompleteReset(t.Context(), resetConsumptionRequest())
		if !errors.Is(err, context.Canceled) || result.Session != nil || result.Status != "" {
			t.Fatalf("late reset: %+v %v", result, err)
		}
	})
	t.Run("invitation", func(t *testing.T) {
		service, memory, _ := consumptionFixture()
		memory.state.Link.Link.Kind = "INVITATION"
		memory.lateErr = context.Canceled
		result, err := service.AcceptInvitation(t.Context(), invitationConsumptionRequest())
		if !errors.Is(err, context.Canceled) || result.CookieToken != "" {
			t.Fatalf("late invitation: %+v %v", result, err)
		}
	})
}

func TestInvitationConsumptionNormalizesAndChecksUsernameBeforeWriting(t *testing.T) {
	service, memory, hasher := consumptionFixture()
	memory.state.Link.Link.Kind = "INVITATION"
	memory.exists = true
	_, err := service.AcceptInvitation(t.Context(), invitationConsumptionRequest())
	if !errors.Is(err, model.ErrUsernameUnavailable) || memory.writes != 0 {
		t.Fatalf("duplicate invitation: %v", err)
	}
	memory.exists = false
	result, err := service.AcceptInvitation(t.Context(), invitationConsumptionRequest())
	if err != nil {
		t.Fatal(err)
	}
	if result.User.Username != "bob" || result.User.DisplayName != "Bob" || result.User.Role != "USER" || memory.invitation.PasswordHash != "prepared-hash" || hasher.calls != 2 {
		t.Fatalf("invited identity: %+v", result.User)
	}
	if memory.invitation.Session.UserID != result.User.UserID || len(memory.audits) != 1 {
		t.Fatal("identity, session and audit mismatch")
	}
}

func TestConsumptionRejectsInvalidTokenWithoutHashOrDatabase(t *testing.T) {
	service, memory, hasher := consumptionFixture()
	service.options.Tokens = linkTokens{}
	_, resetErr := service.CompleteReset(t.Context(), resetConsumptionRequest())
	_, inviteErr := service.AcceptInvitation(t.Context(), invitationConsumptionRequest())
	if !errors.Is(resetErr, model.ErrAccountLinkUnavailable) || !errors.Is(inviteErr, model.ErrAccountLinkUnavailable) || memory.snapshots != 0 || memory.transactions != 0 || hasher.calls != 0 {
		t.Fatalf("invalid capability reached dependencies: %v %v", resetErr, inviteErr)
	}
}
