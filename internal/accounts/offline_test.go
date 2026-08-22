package accounts

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/config"
	"retrom/internal/testassert"
)

func TestReadSetupCodeIsReadOnlyAndPendingOnly(t *testing.T) {
	t.Parallel()
	fixture := newAccountFixture(t, config.ModeRelease)
	if err := fixture.service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	code, err := ReadSetupCode(context.Background(), fixture.database.SQL, fixture.credentials)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return code != fixture.credentials.SetupCode() }), "pending setup code = %q, %v", code, err)
	if _, err := fixture.service.Initialize(context.Background(), InitializeRequest{
		SetupCode: code, Username: "admin", DisplayName: "Administrator",
		Password: "a sufficiently long phrase", PasswordConfirmation: "a sufficiently long phrase",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSetupCode(
		context.Background(), fixture.database.SQL, fixture.credentials,
	); !errors.Is(err, ErrInitializationDone) {
		t.Fatalf("completed setup code = %v", err)
	}
}

func TestOfflineAdminResetRotatesCredentialAndSecurityState(t *testing.T) {
	t.Parallel()
	fixture := newAccountFixture(t, config.ModeTest)
	admin := authenticatedTestAdmin(t, fixture)
	reset, _, err := fixture.service.CreatePasswordReset(
		context.Background(), admin.Principal, admin.User.UserID, 1, uuid.NewString(),
	)
	testassert.False(t, err != nil, err)
	if err := fixture.service.OfflineAdminReset(
		context.Background(), "test", "an offline replacement phrase", "an offline replacement phrase",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Authenticate(
		context.Background(), admin.CookieToken,
	); !errors.Is(err, ErrAuthenticationNeeded) {
		t.Fatalf("old offline recovery session = %v", err)
	}
	if _, err := fixture.service.InspectAccountLink(
		context.Background(), "PASSWORD_RESET", reset.CapabilityToken,
	); !errors.Is(err, ErrAccountLinkUnavailable) {
		t.Fatalf("offline recovery reset link = %v", err)
	}
	if _, err := fixture.service.Login(
		context.Background(), "test", "an offline replacement phrase",
	); err != nil {
		t.Fatalf("offline recovery login = %v", err)
	}
	var defaultActive, audits int
	if err := fixture.database.SQL.QueryRowContext(context.Background(), `
SELECT test_default_password_active,
(SELECT count(*) FROM audit_events
 WHERE actor_kind='SYSTEM' AND actor_label='offline-recovery' AND action='ADMIN_OFFLINE_RECOVERED')
FROM instance_state WHERE id=1
`).Scan(&defaultActive, &audits); err != nil || defaultActive != 0 || audits != 1 {
		t.Fatalf("offline recovery state = default=%d audits=%d error=%v", defaultActive, audits, err)
	}

	member := acceptFixtureInvitation(t, fixture, admin.Principal, "USER", "member", "Member")
	if err := fixture.service.OfflineAdminReset(
		context.Background(), member.User.Username, "another replacement phrase", "another replacement phrase",
	); !errors.Is(err, ErrOfflineAdmin) {
		t.Fatalf("offline recovery accepted USER = %v", err)
	}
}
