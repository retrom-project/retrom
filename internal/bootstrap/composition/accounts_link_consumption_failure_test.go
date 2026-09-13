package composition

import (
	"errors"
	"testing"

	"retrom/internal/bootstrap/config"
	accountservice "retrom/internal/service/accounts"
)

func TestPasswordResetRollsBackWhenDefaultCredentialFlagFails(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	admin := authenticatedTestAdmin(t, fixture)
	link, _, err := fixture.service.CreatePasswordReset(t.Context(), admin.Principal, admin.User.UserID, 1, "reset-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.SQL.ExecContext(t.Context(), `DROP TABLE instance_state`); err != nil {
		t.Fatal(err)
	}
	_, err = fixture.service.CompletePasswordReset(t.Context(), accountservice.CompletePasswordResetRequest{Token: link.CapabilityToken, Password: compliantTestPassword, PasswordConfirmation: compliantTestPassword})
	if err == nil {
		t.Fatal("password reset committed despite failing default credential cleanup")
	}
	if _, err := fixture.service.Authenticate(t.Context(), admin.CookieToken); err != nil {
		t.Fatalf("failed reset revoked old session: %v", err)
	}
	if _, err := fixture.service.InspectAccountLink(t.Context(), "PASSWORD_RESET", link.CapabilityToken); err != nil {
		t.Fatalf("failed reset consumed capability: %v", err)
	}
}

func TestInvitationConsumptionPreservesDatabaseFailure(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	admin := authenticatedTestAdmin(t, fixture)
	link, _, err := fixture.service.CreateInvitation(t.Context(), admin.Principal, "USER", false, "invitation")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.SQL.ExecContext(t.Context(), `DROP TABLE account_links`); err != nil {
		t.Fatal(err)
	}
	_, err = fixture.service.AcceptInvitation(t.Context(), accountservice.AcceptInvitationRequest{Token: link.CapabilityToken, Username: "alice", DisplayName: "Alice", Password: compliantTestPassword, PasswordConfirmation: compliantTestPassword})
	if err == nil || errors.Is(err, accountservice.ErrAccountLinkUnavailable) {
		t.Fatalf("storage failure classified as unavailable invitation: %v", err)
	}
}
