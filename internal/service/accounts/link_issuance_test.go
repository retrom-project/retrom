package accounts

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type issueMemory struct {
	target               LinkTarget
	replay               AccountReplay
	plan                 LinkIssuePlan
	receipt              AccountReceipt
	writes, transactions int
	lateError            error
}

func (memory *issueMemory) WithIssueWrite(_ context.Context, work func(LinkIssueScope) error) error {
	memory.transactions++
	if err := work(LinkIssueScope{Read: memory, Write: memory}); err != nil {
		return err
	}
	return memory.lateError
}

func (memory *issueMemory) Target(context.Context, string) (LinkTarget, bool, error) {
	return memory.target, true, nil
}

func (memory *issueMemory) Replay(context.Context, AccountOperation) (AccountReplay, error) {
	return memory.replay, nil
}

func (memory *issueMemory) Issue(_ context.Context, plan LinkIssuePlan) error {
	memory.plan = plan
	memory.writes++
	return nil
}
func (memory *issueMemory) Audit(context.Context, AccountAudit) error { return nil }
func (memory *issueMemory) Remember(_ context.Context, receipt AccountReceipt) error {
	memory.receipt = receipt
	return nil
}

type issueTokens struct{ calls int }

func (tokens *issueTokens) AccountLinkToken(kind string, id uuid.UUID) string {
	tokens.calls++
	return "secret-" + kind + id.String()
}

func issuanceFixture() (*LinkIssuanceService, *issueMemory, *issueTokens) {
	memory := &issueMemory{target: LinkTarget{User: User{UserID: "target"}, Status: "ENABLED", Version: 4}}
	tokens := &issueTokens{}
	return NewLinkIssuance(memory, tokens, func() time.Time { return time.UnixMilli(100) }), memory, tokens
}

func TestInvitationIssuanceRequiresExplicitAdminConfirmation(t *testing.T) {
	service, memory, _ := issuanceFixture()
	_, _, err := service.Invitation(t.Context(), LinkCreator{UserID: "actor"}, "ADMIN", false, "key")
	if !errors.Is(err, ErrRoleConfirmation) || memory.transactions != 0 {
		t.Fatalf("unconfirmed invitation: %v", err)
	}
}

func TestInvitationIssuanceReplayIsSecretlessAndStable(t *testing.T) {
	service, memory, tokens := issuanceFixture()
	actor := LinkCreator{UserID: "actor", Username: "admin"}
	first, replayed, err := service.Invitation(t.Context(), actor, "USER", false, "key")
	if err != nil || replayed {
		t.Fatalf("invitation: %v replay=%v", err, replayed)
	}
	if strings.Contains(string(memory.receipt.Body), "secret-") {
		t.Fatal("capability persisted in replay")
	}
	memory.replay = AccountReplay{Found: true, Digest: memory.receipt.Operation.Digest, Body: memory.receipt.Body}
	second, replayed, err := service.Invitation(t.Context(), actor, "USER", false, "key")
	if err != nil || !replayed || second.CapabilityToken != first.CapabilityToken {
		t.Fatalf("invitation replay: %v replay=%v", err, replayed)
	}
	if memory.writes != 1 || tokens.calls != 2 || first.ExpiresAtMS != 100+int64(time.Hour/time.Millisecond) {
		t.Fatal("replay repeated write or changed expiry")
	}
}

func TestPasswordResetIssuanceChecksVersionBeforeRevokingOldLinks(t *testing.T) {
	service, memory, _ := issuanceFixture()
	_, _, err := service.PasswordReset(t.Context(), LinkCreator{UserID: "actor"}, "target", 3, "key")
	if !errors.Is(err, ErrUserVersion) || memory.writes != 0 {
		t.Fatalf("stale reset issuance: %v", err)
	}
	link, _, err := service.PasswordReset(t.Context(), LinkCreator{UserID: "actor"}, "target", 4, "key")
	if err != nil {
		t.Fatal(err)
	}
	if !memory.plan.RevokePrevious || link.TargetVersion != 5 || memory.plan.Target.Version != 4 {
		t.Fatalf("reset issuance plan: %+v", memory.plan)
	}
}

func TestLinkIssuanceLateFailureDoesNotExposeCapability(t *testing.T) {
	service, memory, tokens := issuanceFixture()
	memory.lateError = context.Canceled
	link, replayed, err := service.Invitation(t.Context(), LinkCreator{UserID: "actor"}, "USER", false, "key")
	if !errors.Is(err, context.Canceled) || link.AccountLinkID != "" || replayed || tokens.calls != 0 {
		t.Fatalf("failed issuance exposed capability: %+v %v", link, err)
	}
}
