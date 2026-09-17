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

func (memory *administrationMemory) CommitUpdateUser(
	_ context.Context, cmd model.UpdateUserCommand,
) (model.UpdateUserResult, error) {
	if memory.replay.Found && memory.replay.Digest != cmd.Operation.Digest {
		return model.UpdateUserResult{}, model.ErrIdempotencyReused
	}
	if memory.replay.Found {
		return model.UpdateUserResult{Replayed: true}, nil
	}
	if err := model.ValidateManagedUser(memory.user, true, cmd.Version); err != nil {
		return model.UpdateUserResult{}, err
	}
	change, err := model.ResolveUserChange(
		memory.user.User, cmd.Patch, cmd.Operation.PrincipalID == cmd.TargetID,
	)
	if err != nil {
		return model.UpdateUserResult{}, err
	}
	if model.RemovesEnabledAdmin(memory.user.User, change.Role, change.Status) && !memory.another {
		return model.UpdateUserResult{}, model.ErrLastAdmin
	}
	memory.update = model.AdministrationUpdate{Before: memory.user, Change: change, Now: cmd.Operation.Now}
	memory.writes++
	memory.user.User.Role = change.Role
	memory.user.User.Status = change.Status
	memory.user.User.Version++
	if memory.lateError != nil {
		return model.UpdateUserResult{}, memory.lateError
	}
	memory.audits = append(memory.audits, model.AccountAudit{Action: "USER_ROLE_CHANGED"})
	memory.receipt = model.AccountReceipt{
		Operation: cmd.Operation, Status: 200,
		ExpiresAt: cmd.Operation.Now + 86400000,
	}
	return model.UpdateUserResult{User: memory.user.User}, nil
}

func (memory *administrationMemory) CommitDeleteUser(
	_ context.Context, cmd model.DeleteUserCommand,
) (bool, error) {
	if memory.replay.Found && memory.replay.Digest != cmd.Operation.Digest {
		return false, model.ErrIdempotencyReused
	}
	if memory.replay.Found {
		return true, nil
	}
	if err := model.ValidateManagedUser(memory.user, true, cmd.Version); err != nil {
		return false, err
	}
	if err := model.ValidateUserDeletion(memory.user.User, cmd.ActorID, cmd.Confirmation); err != nil {
		return false, err
	}
	if model.RemovesEnabledAdmin(memory.user.User, memory.user.User.Role, "DELETED") && !memory.another {
		return false, model.ErrLastAdmin
	}
	memory.deletion = model.AdministrationDeletion{
		Before: memory.user,
		Security: model.UserSecurity{
			Reason: "USER_DELETED", Sessions: true,
			CreatedLinks: true, TargetLinks: true, Launches: true,
		},
		ClearTestDefault: memory.user.User.Username == "test",
		Now:              cmd.Operation.Now,
	}
	memory.writes++
	memory.audits = append(memory.audits, model.AccountAudit{Action: "USER_DELETED"})
	memory.receipt = model.AccountReceipt{Operation: cmd.Operation, Status: 204}
	return false, memory.lateError
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
