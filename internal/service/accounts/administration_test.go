package accounts

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/accounts"
)

type administrationMemory struct {
	user      model.ManagedUser
	another   bool
	replay    model.AccountReplay
	update    model.AdministrationUpdate
	deletion  model.AdministrationDeletion
	audits    []model.AccountAudit
	receipt   model.AccountReceipt
	writes    int
	lateError error
}

func (memory *administrationMemory) WithWrite(_ context.Context, work func(model.AdministrationScope) error) error {
	if err := work(model.AdministrationScope{Read: memory, Write: memory}); err != nil {
		return err
	}
	return memory.lateError
}

func (memory *administrationMemory) Current(context.Context, string, int64) (model.ManagedUser, bool, error) {
	return memory.user, true, nil
}

func (memory *administrationMemory) AnotherEnabledAdmin(context.Context, string) (bool, error) {
	return memory.another, nil
}

func (memory *administrationMemory) Replay(context.Context, model.AccountOperation) (model.AccountReplay, error) {
	return memory.replay, nil
}

func (memory *administrationMemory) Update(_ context.Context, plan model.AdministrationUpdate) error {
	memory.update = plan
	memory.writes++
	memory.user.User.Role = plan.Change.Role
	memory.user.User.Status = plan.Change.Status
	memory.user.User.Version++
	return nil
}

func (memory *administrationMemory) Delete(_ context.Context, plan model.AdministrationDeletion) error {
	memory.deletion = plan
	memory.writes++
	return nil
}

func (memory *administrationMemory) Audit(_ context.Context, audit model.AccountAudit) error {
	memory.audits = append(memory.audits, audit)
	return nil
}

func (memory *administrationMemory) Remember(_ context.Context, receipt model.AccountReceipt) error {
	memory.receipt = receipt
	return nil
}

func administrationFixture() (*AdministrationService, *administrationMemory) {
	memory := &administrationMemory{user: model.ManagedUser{User: model.AdminUser{UserID: "target", Username: "test", Role: "ADMIN", Status: "ENABLED", Version: 4}, ProfileID: "profile"}, another: true}
	return NewAdministration(memory, func() time.Time { return time.UnixMilli(100) }), memory
}

func TestAdministrationProtectsSelfAndLastAdministrator(t *testing.T) {
	disabled := "DISABLED"
	for _, test := range []struct {
		name, actor string
		another     bool
		want        error
	}{
		{"self", "target", true, model.ErrUserSelfChange}, {"last", "operator", false, model.ErrLastAdmin},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, memory := administrationFixture()
			memory.another = test.another
			_, _, err := service.Update(t.Context(), test.actor, "target", 4, model.UserPatch{Status: &disabled}, "key")
			if !errors.Is(err, test.want) || memory.writes != 0 {
				t.Fatalf("protected mutation: %v writes=%d", err, memory.writes)
			}
		})
	}
}

func TestAdministrationPlansSecurityAndAuditInSameScope(t *testing.T) {
	service, memory := administrationFixture()
	role := "USER"
	after, replayed, err := service.Update(t.Context(), "operator", "target", 4, model.UserPatch{Role: &role}, "key")
	if err != nil || replayed {
		t.Fatalf("update: %v replay=%v", err, replayed)
	}
	security := memory.update.Change.Security
	if !security.Sessions || !security.CreatedLinks || security.TargetLinks || security.Launches {
		t.Fatalf("downgrade security: %+v", security)
	}
	if after.Version != 5 || len(memory.audits) != 1 || memory.audits[0].Action != "USER_ROLE_CHANGED" {
		t.Fatalf("update/audit: %+v %+v", after, memory.audits)
	}
	if memory.receipt.ExpiresAt != 100+int64(24*time.Hour/time.Millisecond) {
		t.Fatal("replay expiry changed")
	}
}

func TestAdministrationLateFailureDoesNotReturnCommittedResult(t *testing.T) {
	service, memory := administrationFixture()
	memory.lateError = context.Canceled
	status := "DISABLED"
	after, replayed, err := service.Update(t.Context(), "operator", "target", 4, model.UserPatch{Status: &status}, "key")
	if !errors.Is(err, context.Canceled) || replayed || after.UserID != "" {
		t.Fatalf("failed commit returned result: %+v %v %v", after, replayed, err)
	}
}

func TestAdministrationDeletionChecksVersionAndConfirmation(t *testing.T) {
	for _, test := range []struct {
		name, confirmation string
		version            int64
		want               error
	}{
		{"version", "test", 3, model.ErrUserVersion}, {"confirmation", " test", 4, model.ErrConfirmation},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, memory := administrationFixture()
			_, err := service.Delete(t.Context(), "operator", "target", test.version, test.confirmation, "key")
			if !errors.Is(err, test.want) || memory.writes != 0 {
				t.Fatalf("deletion guard: %v writes=%d", err, memory.writes)
			}
		})
	}
}

func TestAdministrationDeletionPlansAllSecurityRevocations(t *testing.T) {
	service, memory := administrationFixture()
	if _, err := service.Delete(t.Context(), "operator", "target", 4, "test", "key"); err != nil {
		t.Fatal(err)
	}
	plan := memory.deletion
	if !plan.ClearTestDefault || !plan.Security.Sessions || !plan.Security.CreatedLinks || !plan.Security.TargetLinks || !plan.Security.Launches {
		t.Fatalf("deletion plan: %+v", plan)
	}
	if len(memory.audits) != 1 || memory.audits[0].Action != "USER_DELETED" || memory.receipt.Status != 204 {
		t.Fatal("missing deletion receipt/audit")
	}
}

func TestAdministrationRejectsConflictingReplayBeforeMutation(t *testing.T) {
	service, memory := administrationFixture()
	memory.replay = model.AccountReplay{Found: true, Digest: "different"}
	_, err := service.Delete(t.Context(), "operator", "target", 4, "test", "key")
	if !errors.Is(err, model.ErrIdempotencyReused) || memory.writes != 0 {
		t.Fatalf("replay conflict: %v", err)
	}
}
