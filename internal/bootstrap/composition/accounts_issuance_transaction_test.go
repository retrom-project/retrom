package composition

import (
	"context"
	"errors"
	"testing"
	"time"

	"retrom/internal/bootstrap/config"
	accountsmodel "retrom/internal/model/accounts"
	accountpersistence "retrom/internal/repo/accounts"
	accountservice "retrom/internal/service/accounts"
)

type failingIssueRepository struct {
	repository accountsmodel.LinkIssueRepository
}

func (repository failingIssueRepository) WithIssueWrite(ctx context.Context, work func(accountsmodel.LinkIssueScope) error) error {
	return repository.repository.WithIssueWrite(ctx, func(scope accountsmodel.LinkIssueScope) error {
		if err := work(scope); err != nil {
			return err
		}
		return context.Canceled
	})
}

func TestPasswordResetIssuanceLateFailureKeepsOldLinkAndVersion(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	admin := authenticatedTestAdmin(t, fixture)
	target := acceptFixtureInvitation(t, fixture, admin.Principal, "USER", "alice", "Alice")
	old, _, err := fixture.service.CreatePasswordReset(t.Context(), admin.Principal, target.User.UserID, 1, "old-reset")
	if err != nil {
		t.Fatal(err)
	}
	service := accountservice.NewLinkIssuance(failingIssueRepository{accountpersistence.NewLinks(fixture.database.SQL)}, fixture.credentials, func() time.Time { return *fixture.now })
	result, _, err := service.PasswordReset(t.Context(), accountsmodel.LinkCreator{UserID: admin.User.UserID, Username: admin.User.Username}, target.User.UserID, 2, "new-reset")
	if !errors.Is(err, context.Canceled) || result.CapabilityToken != "" {
		t.Fatalf("late issuance: %+v %v", result, err)
	}
	if _, err := fixture.service.InspectAccountLink(t.Context(), "PASSWORD_RESET", old.CapabilityToken); err != nil {
		t.Fatalf("failed issuance revoked old capability: %v", err)
	}
	user, err := fixture.service.GetUser(t.Context(), target.User.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if user.Version != 2 {
		t.Fatalf("failed issuance advanced user version: %d", user.Version)
	}
	var links, replays, audits int
	if err := fixture.database.SQL.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM account_links WHERE target_user_id=?),(SELECT count(*) FROM idempotency_records WHERE key='new-reset'),(SELECT count(*) FROM audit_events WHERE action='PASSWORD_RESET_CREATED')`, target.User.UserID).Scan(&links, &replays, &audits); err != nil {
		t.Fatal(err)
	}
	if links != 1 || replays != 0 || audits != 1 {
		t.Fatalf("partial issuance: links=%d replays=%d audits=%d", links, replays, audits)
	}
}
