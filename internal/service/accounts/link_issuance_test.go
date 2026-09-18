package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	model "retrom/internal/model/accounts"

	"github.com/google/uuid"
)

type issueMemory struct {
	target    model.LinkTarget
	replay    model.AccountReplay
	cmd       model.LinkIssueCommand
	writes    int
	commitErr error
}

func (memory *issueMemory) CommitIssue(
	_ context.Context, cmd model.LinkIssueCommand,
) (model.LinkIssueResult, error) {
	memory.cmd = cmd
	if err := model.CheckAccountReplay(memory.replay, cmd.Operation); err != nil {
		return model.LinkIssueResult{}, err
	}
	if memory.replay.Found {
		var link model.AccountLink
		if err := json.Unmarshal(memory.replay.Body, &link); err != nil {
			return model.LinkIssueResult{}, err
		}
		return model.LinkIssueResult{Link: link, Replayed: true}, nil
	}
	if cmd.Plan.Target != nil {
		if err := model.ValidateLinkTarget(
			memory.target, true, cmd.Plan.Target.Version,
		); err != nil {
			return model.LinkIssueResult{}, err
		}
	}
	if memory.commitErr != nil {
		return model.LinkIssueResult{}, memory.commitErr
	}
	memory.writes++
	return model.LinkIssueResult{Link: cmd.Plan.Link, Replayed: false}, nil
}

type issueTokens struct{ calls int }

func (tokens *issueTokens) AccountLinkToken(kind string, id uuid.UUID) string {
	tokens.calls++
	return "secret-" + kind + id.String()
}

func issuanceFixture() (*LinkIssuanceService, *issueMemory, *issueTokens) {
	memory := &issueMemory{target: model.LinkTarget{
		User: model.User{UserID: "target"}, Status: "ENABLED", Version: 4,
	}}
	tokens := &issueTokens{}
	return NewLinkIssuance(
		memory, tokens, func() time.Time { return time.UnixMilli(100) },
	), memory, tokens
}

func TestInvitationIssuanceRequiresExplicitAdminConfirmation(t *testing.T) {
	service, memory, _ := issuanceFixture()
	_, _, err := service.Invitation(
		t.Context(), model.LinkCreator{UserID: "actor"}, "ADMIN", false, "key",
	)
	if !errors.Is(err, model.ErrRoleConfirmation) || memory.writes != 0 {
		t.Fatalf("unconfirmed invitation: %v", err)
	}
}

func TestInvitationIssuanceReplayIsSecretlessAndStable(t *testing.T) {
	service, memory, tokens := issuanceFixture()
	actor := model.LinkCreator{UserID: "actor", Username: "admin"}
	first, replayed, err := service.Invitation(t.Context(), actor, "USER", false, "key")
	if err != nil || replayed {
		t.Fatalf("invitation: %v replay=%v", err, replayed)
	}
	receiptBody := memory.cmd.Receipt.Body
	if strings.Contains(string(receiptBody), "secret-") {
		t.Fatal("capability persisted in replay")
	}
	memory.replay = model.AccountReplay{
		Found: true, Digest: memory.cmd.Receipt.Operation.Digest,
		Body: receiptBody,
	}
	second, replayed, err := service.Invitation(t.Context(), actor, "USER", false, "key")
	if err != nil || !replayed || second.CapabilityToken != first.CapabilityToken {
		t.Fatalf("invitation replay: %v replay=%v", err, replayed)
	}
	if memory.writes != 1 || tokens.calls != 2 ||
		first.ExpiresAtMS != 100+int64(time.Hour/time.Millisecond) {
		t.Fatal("replay repeated write or changed expiry")
	}
}

func TestPasswordResetIssuanceChecksVersionBeforeRevokingOldLinks(t *testing.T) {
	service, memory, _ := issuanceFixture()
	_, _, err := service.PasswordReset(
		t.Context(), model.LinkCreator{UserID: "actor"}, "target", 3, "key",
	)
	if !errors.Is(err, model.ErrUserVersion) || memory.writes != 0 {
		t.Fatalf("stale reset issuance: %v", err)
	}
	link, _, err := service.PasswordReset(
		t.Context(), model.LinkCreator{UserID: "actor"}, "target", 4, "key",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !memory.cmd.Plan.RevokePrevious || link.TargetVersion != 5 ||
		memory.cmd.Plan.Target.Version != 4 {
		t.Fatalf("reset issuance plan: %+v", memory.cmd.Plan)
	}
}

func TestLinkIssuanceLateFailureDoesNotExposeCapability(t *testing.T) {
	service, memory, tokens := issuanceFixture()
	memory.commitErr = context.Canceled
	link, replayed, err := service.Invitation(
		t.Context(), model.LinkCreator{UserID: "actor"}, "USER", false, "key",
	)
	if !errors.Is(err, context.Canceled) || link.AccountLinkID != "" || replayed ||
		tokens.calls != 0 {
		t.Fatalf("failed issuance exposed capability: %+v %v", link, err)
	}
}
