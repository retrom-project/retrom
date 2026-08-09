package accounts

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/config"
)

const compliantTestPassword = "a sufficiently long passphrase"

func authenticatedTestAdmin(t *testing.T, fixture accountFixture) Session {
	t.Helper()
	if err := fixture.service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	session, err := fixture.service.Login(context.Background(), "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func acceptFixtureInvitation(
	t *testing.T,
	fixture accountFixture,
	principal authn.Principal,
	role, username, displayName string,
) Session {
	t.Helper()
	link, _, err := fixture.service.CreateInvitation(
		context.Background(), principal, role, role == "ADMIN", uuid.NewString(),
	)
	if err != nil {
		t.Fatal(err)
	}
	session, err := fixture.service.AcceptInvitation(context.Background(), AcceptInvitationRequest{
		Token: link.CapabilityToken, Username: username, DisplayName: displayName,
		Password: compliantTestPassword, PasswordConfirmation: compliantTestPassword,
	})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestInvitationAndPasswordResetCapabilitiesAreSingleUseAndSecretless(t *testing.T) {
	t.Parallel()
	fixture := newAccountFixture(t, config.ModeTest)
	admin := authenticatedTestAdmin(t, fixture)
	key := uuid.NewString()
	invitation, replayed, err := fixture.service.CreateInvitation(
		context.Background(), admin.Principal, "USER", false, key,
	)
	if err != nil || replayed || len(invitation.CapabilityToken) != 64 {
		t.Fatalf("create invitation = %#v replay=%v error=%v", invitation, replayed, err)
	}
	replay, replayed, err := fixture.service.CreateInvitation(
		context.Background(), admin.Principal, "USER", false, key,
	)
	if err != nil || !replayed || replay.AccountLinkID != invitation.AccountLinkID ||
		replay.CapabilityToken != invitation.CapabilityToken {
		t.Fatalf("replay invitation = %#v replay=%v error=%v", replay, replayed, err)
	}
	if _, _, err := fixture.service.CreateInvitation(
		context.Background(), admin.Principal, "ADMIN", false, uuid.NewString(),
	); !errors.Is(err, ErrRoleConfirmation) {
		t.Fatalf("unconfirmed admin invitation = %v", err)
	}
	inspection, err := fixture.service.InspectAccountLink(
		context.Background(), "INVITATION", invitation.CapabilityToken,
	)
	if err != nil || inspection.Role != "USER" || inspection.Username != nil {
		t.Fatalf("invitation inspection = %#v, %v", inspection, err)
	}
	registered, err := fixture.service.AcceptInvitation(context.Background(), AcceptInvitationRequest{
		Token: invitation.CapabilityToken, Username: "alice", DisplayName: "Alice",
		Password: compliantTestPassword, PasswordConfirmation: compliantTestPassword,
	})
	if err != nil || registered.User.Username != "alice" {
		t.Fatalf("accept invitation = %#v, %v", registered, err)
	}
	if _, err := fixture.service.InspectAccountLink(
		context.Background(), "INVITATION", invitation.CapabilityToken,
	); !errors.Is(err, ErrAccountLinkUnavailable) {
		t.Fatalf("consumed invitation inspection = %v", err)
	}
	if _, err := fixture.service.AcceptInvitation(context.Background(), AcceptInvitationRequest{
		Token: invitation.CapabilityToken, Username: "alice2", DisplayName: "Alice 2",
		Password: compliantTestPassword, PasswordConfirmation: compliantTestPassword,
	}); !errors.Is(err, ErrAccountLinkUnavailable) {
		t.Fatalf("invitation second use = %v", err)
	}

	reset, _, err := fixture.service.CreatePasswordReset(
		context.Background(), admin.Principal, registered.User.UserID, 1, uuid.NewString(),
	)
	if err != nil || len(reset.CapabilityToken) != 64 || reset.TargetVersion != 2 {
		t.Fatalf("create password reset = %#v, %v", reset, err)
	}
	resetInspection, err := fixture.service.InspectAccountLink(
		context.Background(), "PASSWORD_RESET", reset.CapabilityToken,
	)
	if err != nil || resetInspection.Username != "alice" || resetInspection.Role != nil {
		t.Fatalf("reset inspection = %#v, %v", resetInspection, err)
	}
	changed, err := fixture.service.CompletePasswordReset(context.Background(), CompletePasswordResetRequest{
		Token: reset.CapabilityToken, Password: "a replacement passphrase", PasswordConfirmation: "a replacement passphrase",
	})
	if err != nil || changed.Session == nil || changed.Status != "AUTHENTICATED" {
		t.Fatalf("complete password reset = %#v, %v", changed, err)
	}
	if _, err := fixture.service.Authenticate(context.Background(), registered.CookieToken); !errors.Is(
		err, ErrAuthenticationNeeded,
	) {
		t.Fatalf("old registration session after reset = %v", err)
	}
	if _, err := fixture.service.Login(context.Background(), "alice", "a replacement passphrase"); err != nil {
		t.Fatalf("login with reset password = %v", err)
	}
	operator := authn.Principal{UserID: uuid.NewString(), Username: "offline-operator"}
	if _, _, err := fixture.service.UpdateUser(
		context.Background(), operator, admin.User.UserID, 1,
		UserPatch{Status: stringPointer("DISABLED")}, uuid.NewString(),
	); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("last enabled admin update = %v", err)
	}
	if _, err := fixture.service.DeleteUser(
		context.Background(), operator, admin.User.UserID, 1, admin.User.Username, uuid.NewString(),
	); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("last enabled admin delete = %v", err)
	}

	var tokenColumns int
	if err := fixture.database.SQL.QueryRow(`
SELECT count(*) FROM pragma_table_info('account_links')
WHERE lower(name) LIKE '%token%' OR lower(name) LIKE '%secret%' OR lower(name) LIKE '%hash%'
`).Scan(&tokenColumns); err != nil || tokenColumns != 0 {
		t.Fatalf("account link secret columns = %d, error=%v", tokenColumns, err)
	}
	var storedBody string
	if err := fixture.database.SQL.QueryRow(`
SELECT CAST(response_body AS TEXT) FROM idempotency_records
WHERE principal_id=? AND operation_id='postAdminInvitation' AND key=?
`, admin.Principal.UserID, key).Scan(&storedBody); err != nil ||
		len(invitation.CapabilityToken) != 0 && strings.Contains(storedBody, invitation.CapabilityToken) {
		t.Fatalf("invitation secret persisted in idempotency body: %q, error=%v", storedBody, err)
	}
}

func TestInvitationConcurrentConsumptionAndUserLifecycleRevocations(t *testing.T) {
	t.Parallel()
	fixture := newAccountFixture(t, config.ModeTest)
	admin := authenticatedTestAdmin(t, fixture)
	conflict, _, err := fixture.service.CreateInvitation(
		context.Background(), admin.Principal, "USER", false, uuid.NewString(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.AcceptInvitation(context.Background(), AcceptInvitationRequest{
		Token: conflict.CapabilityToken, Username: "test", DisplayName: "Duplicate",
		Password: compliantTestPassword, PasswordConfirmation: compliantTestPassword,
	}); !errors.Is(err, ErrUsernameUnavailable) {
		t.Fatalf("duplicate username = %v", err)
	}
	if _, err := fixture.service.InspectAccountLink(
		context.Background(), "INVITATION", conflict.CapabilityToken,
	); err != nil {
		t.Fatalf("username conflict consumed invitation: %v", err)
	}

	concurrent, _, err := fixture.service.CreateInvitation(
		context.Background(), admin.Principal, "USER", false, uuid.NewString(),
	)
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		session Session
		err     error
	}
	outcomes := make(chan outcome, 2)
	var wait sync.WaitGroup
	for _, username := range []string{"racer-a", "racer-b"} {
		wait.Add(1)
		go func(username string) {
			defer wait.Done()
			session, acceptErr := fixture.service.AcceptInvitation(context.Background(), AcceptInvitationRequest{
				Token: concurrent.CapabilityToken, Username: username,
				DisplayName: "Racer", Password: compliantTestPassword,
				PasswordConfirmation: compliantTestPassword,
			})
			outcomes <- outcome{session: session, err: acceptErr}
		}(username)
	}
	wait.Wait()
	close(outcomes)
	var winner Session
	var successes, unavailable int
	for result := range outcomes {
		switch {
		case result.err == nil:
			successes++
			winner = result.session
		case errors.Is(result.err, ErrAccountLinkUnavailable):
			unavailable++
		default:
			t.Fatalf("unexpected concurrent result: %v", result.err)
		}
	}
	if successes != 1 || unavailable != 1 {
		t.Fatalf("concurrent invitation outcomes = %d/%d", successes, unavailable)
	}

	reset, _, err := fixture.service.CreatePasswordReset(
		context.Background(), admin.Principal, winner.User.UserID, 1, uuid.NewString(),
	)
	if err != nil {
		t.Fatal(err)
	}
	disabled, _, err := fixture.service.UpdateUser(
		context.Background(), admin.Principal, winner.User.UserID, 2,
		UserPatch{Status: stringPointer("DISABLED")}, uuid.NewString(),
	)
	if err != nil || disabled.Status != "DISABLED" || disabled.Version != 3 {
		t.Fatalf("disable user = %#v, %v", disabled, err)
	}
	if _, err := fixture.service.Authenticate(context.Background(), winner.CookieToken); !errors.Is(
		err, ErrAuthenticationNeeded,
	) {
		t.Fatalf("disabled user session = %v", err)
	}
	if _, err := fixture.service.InspectAccountLink(
		context.Background(), "PASSWORD_RESET", reset.CapabilityToken,
	); !errors.Is(err, ErrAccountLinkUnavailable) {
		t.Fatalf("disabled user reset link = %v", err)
	}
	disabledReset, _, err := fixture.service.CreatePasswordReset(
		context.Background(), admin.Principal, winner.User.UserID, 3, uuid.NewString(),
	)
	if err != nil {
		t.Fatal(err)
	}
	resetResult, err := fixture.service.CompletePasswordReset(context.Background(), CompletePasswordResetRequest{
		Token: disabledReset.CapabilityToken, Password: "disabled account passphrase",
		PasswordConfirmation: "disabled account passphrase",
	})
	if err != nil || resetResult.Session != nil || resetResult.Status != "PASSWORD_CHANGED_ACCOUNT_DISABLED" {
		t.Fatalf("disabled reset = %#v, %v", resetResult, err)
	}
	if _, err := fixture.service.Login(context.Background(), winner.User.Username, "disabled account passphrase"); !errors.Is(
		err, ErrAuthentication,
	) {
		t.Fatalf("disabled login = %v", err)
	}

	current, err := fixture.service.GetUser(context.Background(), winner.User.UserID)
	if err != nil {
		t.Fatal(err)
	}
	enabled, _, err := fixture.service.UpdateUser(
		context.Background(), admin.Principal, winner.User.UserID, current.Version,
		UserPatch{Status: stringPointer("ENABLED")}, uuid.NewString(),
	)
	if err != nil || enabled.Status != "ENABLED" {
		t.Fatalf("enable user = %#v, %v", enabled, err)
	}
	if _, err := fixture.service.Login(context.Background(), winner.User.Username, "disabled account passphrase"); err != nil {
		t.Fatalf("enabled login = %v", err)
	}
	replayed, err := fixture.service.DeleteUser(
		context.Background(), admin.Principal, winner.User.UserID, enabled.Version,
		winner.User.Username, uuid.NewString(),
	)
	if err != nil || replayed {
		t.Fatalf("delete user = replay=%v error=%v", replayed, err)
	}
	var credentials int
	if err := fixture.database.SQL.QueryRow(
		`SELECT count(*) FROM user_credentials WHERE user_id=?`, winner.User.UserID,
	).Scan(&credentials); err != nil || credentials != 0 {
		t.Fatalf("deleted credential count = %d, error=%v", credentials, err)
	}

	otherAdmin := acceptFixtureInvitation(t, fixture, admin.Principal, "ADMIN", "other-admin", "Other Admin")
	if _, _, err := fixture.service.UpdateUser(
		context.Background(), otherAdmin.Principal, otherAdmin.User.UserID, 1,
		UserPatch{Status: stringPointer("DISABLED")}, uuid.NewString(),
	); !errors.Is(err, ErrUserSelfChange) {
		t.Fatalf("self-management guard = %v", err)
	}
}

func stringPointer(value string) *string { return &value }
