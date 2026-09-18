package composition

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"retrom/internal/bootstrap/config"
	"retrom/internal/capability/security/authn"

	accountsmodel "retrom/internal/model/accounts"
	accountpersistence "retrom/internal/repo/accounts"
	accountsservice "retrom/internal/service/accounts"
)

func failingConsumptionService(fixture accountFixture) *accountsservice.LinkConsumptionService {
	repo := accountpersistence.NewLinks(fixture.database.SQL)
	repo.WithPreCommitHook(func() error { return context.Canceled })
	return accountsservice.NewLinkConsumption(repo, accountsmodel.LinkConsumptionOptions{
		Tokens: fixture.credentials, Hasher: authn.NewPasswordHasher(), Blocklist: authn.EmptyBlocklist{}, Mint: func() (accountsmodel.SessionMaterial, error) { return accountsservice.MintSession(rand.Reader) }, Now: func() time.Time { return *fixture.now },
	})
}

func TestInvitationConsumptionLateFailureRollsBackIdentityAndAudit(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	admin := authenticatedTestAdmin(t, fixture)
	link, _, err := fixture.service.CreateInvitation(t.Context(), admin.Principal, "USER", false, "late-invite")
	if err != nil {
		t.Fatal(err)
	}
	var beforeSessions int
	if err := fixture.database.SQL.QueryRowContext(t.Context(), `SELECT count(*) FROM auth_sessions`).Scan(&beforeSessions); err != nil {
		t.Fatal(err)
	}
	result, err := failingConsumptionService(fixture).AcceptInvitation(t.Context(), accountsmodel.AcceptInvitationRequest{Token: link.CapabilityToken, Username: "alice", DisplayName: "Alice", Password: compliantTestPassword, PasswordConfirmation: compliantTestPassword})
	if !errors.Is(err, context.Canceled) || result.CookieToken != "" {
		t.Fatalf("late invitation: %+v %v", result, err)
	}
	var users, profiles, sessions, audits int
	if err := fixture.database.SQL.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM users),(SELECT count(*) FROM profiles),(SELECT count(*) FROM auth_sessions),(SELECT count(*) FROM audit_events WHERE action='INVITATION_ACCEPTED')`).Scan(&users, &profiles, &sessions, &audits); err != nil {
		t.Fatal(err)
	}
	if users != 1 || profiles != 1 || sessions != beforeSessions || audits != 0 {
		t.Fatalf("partial invitation: users=%d profiles=%d sessions=%d audits=%d", users, profiles, sessions, audits)
	}
	if _, err := fixture.service.InspectAccountLink(t.Context(), "INVITATION", link.CapabilityToken); err != nil {
		t.Fatalf("failed invitation consumed capability: %v", err)
	}
}

func TestPasswordResetConsumptionLateFailureRollsBackCredentialAndAudit(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	admin := authenticatedTestAdmin(t, fixture)
	link, _, err := fixture.service.CreatePasswordReset(t.Context(), admin.Principal, admin.User.UserID, 1, "late-reset")
	if err != nil {
		t.Fatal(err)
	}
	result, err := failingConsumptionService(fixture).CompleteReset(t.Context(), accountsmodel.CompletePasswordResetRequest{Token: link.CapabilityToken, Password: compliantTestPassword, PasswordConfirmation: compliantTestPassword})
	if !errors.Is(err, context.Canceled) || result.Session != nil {
		t.Fatalf("late reset: %+v %v", result, err)
	}
	if _, err := fixture.service.Authenticate(t.Context(), admin.CookieToken); err != nil {
		t.Fatalf("failed reset revoked session: %v", err)
	}
	if _, err := fixture.service.Login(t.Context(), "test", "test"); err != nil {
		t.Fatalf("failed reset changed credential: %v", err)
	}
	if _, err := fixture.service.InspectAccountLink(t.Context(), "PASSWORD_RESET", link.CapabilityToken); err != nil {
		t.Fatalf("failed reset consumed capability: %v", err)
	}
	var version, audits int
	var defaultActive bool
	if err := fixture.database.SQL.QueryRowContext(t.Context(), `SELECT version,(SELECT count(*) FROM audit_events WHERE action='PASSWORD_RESET_COMPLETED'),(SELECT test_default_password_active FROM instance_state) FROM users WHERE id=?`, admin.User.UserID).Scan(&version, &audits, &defaultActive); err != nil {
		t.Fatal(err)
	}
	if version != 2 || audits != 0 || !defaultActive {
		t.Fatalf("partial reset: version=%d audits=%d default=%v", version, audits, defaultActive)
	}
}
