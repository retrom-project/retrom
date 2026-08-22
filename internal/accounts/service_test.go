package accounts

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/config"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/store"
	"retrom/internal/testassert"
)

type accountFixture struct {
	service     *Service
	credentials *retromruntime.Credentials
	database    *store.DB
	now         *time.Time
}

func newAccountFixture(t *testing.T, mode config.Mode) accountFixture {
	t.Helper()
	root := t.TempDir()
	fixed := time.UnixMilli(1_786_000_000_000).UTC()
	database, err := store.Open(context.Background(), filepath.Join(root, "retrom.db"), func() time.Time { return fixed })
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	credentials, err := retromruntime.LoadOrCreateCredentials(root)
	testassert.False(t, err != nil, err)
	service, err := New(context.Background(), database.SQL, credentials, mode, authn.EmptyBlocklist{}, func() time.Time { return fixed })
	testassert.False(t, err != nil, err)
	return accountFixture{service: service, credentials: credentials, database: database, now: &fixed}
}

func TestTestModeBootstrapsExactlyOnceAndReleaseRejectsDefaultCredential(t *testing.T) {
	t.Parallel()
	fixture := newAccountFixture(t, config.ModeTest)
	if err := fixture.service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.Start(context.Background()); err != nil {
		t.Fatalf("second Start() = %v", err)
	}
	var users, profiles, credentials int
	var kind string
	var active int
	if err := fixture.database.SQL.QueryRowContext(context.Background(), `
SELECT (SELECT count(*) FROM users),(SELECT count(*) FROM profiles),
(SELECT count(*) FROM user_credentials),bootstrap_kind,test_default_password_active
FROM instance_state WHERE id=1
`).Scan(&users, &profiles, &credentials, &kind, &active); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return users != 1 }, func() bool { return profiles != 1 }, func() bool { return credentials != 1 }, func() bool { return kind != "TEST_DEFAULT" }, func() bool { return active != 1 }), "bootstrap = %d/%d/%d %s/%d", users, profiles, credentials, kind, active)
	var actorKind, actorLabel string
	var actorUserID any
	if err := fixture.database.SQL.QueryRowContext(context.Background(), `
SELECT actor_kind,actor_user_id,actor_label FROM audit_events WHERE action='INSTANCE_INITIALIZED'
`).Scan(&actorKind, &actorUserID, &actorLabel); err != nil || actorKind != "SYSTEM" || actorUserID != nil ||
		actorLabel != "startup-test-bootstrap" {
		t.Fatalf("bootstrap actor = %s/%v/%s, error=%v", actorKind, actorUserID, actorLabel, err)
	}
	loggedIn, err := fixture.service.Login(context.Background(), "test", "test")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return loggedIn.User.Username != "test" }, func() bool { return loggedIn.User.Role != "ADMIN" }), "test login = %#v, %v", loggedIn.User, err)
	release, err := New(
		context.Background(), fixture.database.SQL, fixture.credentials, config.ModeRelease,
		authn.EmptyBlocklist{}, func() time.Time { return *fixture.now },
	)
	testassert.False(t, err != nil, err)
	if err := release.Start(context.Background()); !errors.Is(err, ErrTestCredential) {
		t.Fatalf("release Start() = %v", err)
	}
}

func TestReleaseInitializationLoginExpiryAndPasswordRotation(t *testing.T) {
	t.Parallel()
	fixture := newAccountFixture(t, config.ModeRelease)
	if err := fixture.service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Initialize(context.Background(), InitializeRequest{
		SetupCode: "invalid", Username: "admin", DisplayName: "Administrator",
		Password: "a sufficiently long phrase", PasswordConfirmation: "a sufficiently long phrase",
	}); !errors.Is(err, ErrInitializationProof) {
		t.Fatalf("invalid setup = %v", err)
	}
	var users int
	if err := fixture.database.SQL.QueryRowContext(context.Background(), "SELECT count(*) FROM users").Scan(&users); err != nil || users != 0 {
		t.Fatalf("users after invalid setup = %d, %v", users, err)
	}
	initialized, err := fixture.service.Initialize(context.Background(), InitializeRequest{
		SetupCode: fixture.credentials.SetupCode(), Username: "admin", DisplayName: "Administrator",
		Password: "a sufficiently long phrase", PasswordConfirmation: "a sufficiently long phrase",
	})
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return initialized.User.Role != "ADMIN" }, func() bool { return initialized.CSRFToken == "" }, func() bool { return initialized.CookieToken == "" }), "initialized session = %#v", initialized)
	otherPassword := strings.Repeat("other phrase ", 2)
	if _, err := fixture.service.Initialize(context.Background(), InitializeRequest{
		SetupCode: fixture.credentials.SetupCode(), Username: "other", DisplayName: "Other Admin",
		Password: otherPassword, PasswordConfirmation: otherPassword,
	}); !errors.Is(err, ErrInitializationDone) {
		t.Fatalf("reinitialize = %v", err)
	}
	if _, err := fixture.service.Login(context.Background(), "missing", "a sufficiently long phrase"); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("missing login = %v", err)
	}
	loggedIn, err := fixture.service.Login(context.Background(), "admin", "a sufficiently long phrase")
	testassert.False(t, err != nil, err)
	authenticated, err := fixture.service.Authenticate(context.Background(), loggedIn.CookieToken)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return authenticated.Principal.UserID != loggedIn.Principal.UserID }, func() bool { return !MatchesCSRF(loggedIn.CookieToken, loggedIn.CSRFToken) }, func() bool { return MatchesCSRF(loggedIn.CookieToken, "wrong") }), "authenticated = %#v, %v", authenticated.Principal, err)
	rotated, err := fixture.service.ChangePassword(
		context.Background(), loggedIn.Principal, "a sufficiently long phrase",
		"a new sufficiently long phrase", "a new sufficiently long phrase",
	)
	testassert.False(t, err != nil, err)
	testassert.False(t, rotated.CookieToken == loggedIn.CookieToken, "password change reused the old session token")
	if _, err := fixture.service.Authenticate(context.Background(), loggedIn.CookieToken); !errors.Is(err, ErrAuthenticationNeeded) {
		t.Fatalf("old session after rotation = %v", err)
	}
	if _, err := fixture.service.Login(context.Background(), "admin", "a sufficiently long phrase"); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("old password login = %v", err)
	}
	if _, err := fixture.service.Login(context.Background(), "admin", "a new sufficiently long phrase"); err != nil {
		t.Fatalf("new password login = %v", err)
	}
	*fixture.now = fixture.now.Add(24 * time.Hour)
	if _, err := fixture.service.Authenticate(context.Background(), rotated.CookieToken); !errors.Is(err, ErrAuthenticationNeeded) {
		t.Fatalf("absolute expiry = %v", err)
	}
}

func TestAuthenticatePreservesDatabaseFailures(t *testing.T) {
	t.Parallel()
	fixture := newAccountFixture(t, config.ModeTest)
	if err := fixture.service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	loggedIn, err := fixture.service.Login(context.Background(), "test", "test")
	testassert.False(t, err != nil, err)
	if err := fixture.database.SQL.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = fixture.service.Authenticate(context.Background(), loggedIn.CookieToken)
	testassert.Falsef(t, testassert.Any(func() bool { return err == nil }, func() bool { return errors.Is(err, ErrAuthenticationNeeded) }, func() bool { return !strings.Contains(err.Error(), "authenticate session") }), "database failure was collapsed to authentication state: %v", err)
}

func TestStartRejectsCorruptCredentialWithoutComputingIt(t *testing.T) {
	t.Parallel()
	fixture := newAccountFixture(t, config.ModeTest)
	if err := fixture.service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.SQL.ExecContext(context.Background(), `
UPDATE user_credentials SET password_hash='$argon2id$v=19$m=999999,t=2,p=1$bad$bad'
`); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.Start(context.Background()); !errors.Is(err, authn.ErrCredential) {
		t.Fatalf("corrupt credential Start() = %v", err)
	}
}

func TestStartRejectsCompletedInstanceWithOrphanProfile(t *testing.T) {
	t.Parallel()
	fixture := newAccountFixture(t, config.ModeTest)
	if err := fixture.service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.SQL.ExecContext(context.Background(), `
INSERT INTO profiles(id,display_name,created_at_ms)
VALUES('01980000-0000-7000-8000-000000000999','Orphan',1786000000000)
`); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.Start(context.Background()); !errors.Is(err, ErrInitializationState) {
		t.Fatalf("orphan profile Start() = %v", err)
	}
}
