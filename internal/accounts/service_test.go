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
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	credentials, err := retromruntime.LoadOrCreateCredentials(root)
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(context.Background(), database.SQL, credentials, mode, authn.EmptyBlocklist{}, func() time.Time { return fixed })
	if err != nil {
		t.Fatal(err)
	}
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
	if err := fixture.database.SQL.QueryRow(`
SELECT (SELECT count(*) FROM users),(SELECT count(*) FROM profiles),
(SELECT count(*) FROM user_credentials),bootstrap_kind,test_default_password_active
FROM instance_state WHERE id=1
`).Scan(&users, &profiles, &credentials, &kind, &active); err != nil {
		t.Fatal(err)
	}
	if users != 1 || profiles != 1 || credentials != 1 || kind != "TEST_DEFAULT" || active != 1 {
		t.Fatalf("bootstrap = %d/%d/%d %s/%d", users, profiles, credentials, kind, active)
	}
	var actorKind, actorLabel string
	var actorUserID any
	if err := fixture.database.SQL.QueryRow(`
SELECT actor_kind,actor_user_id,actor_label FROM audit_events WHERE action='INSTANCE_INITIALIZED'
`).Scan(&actorKind, &actorUserID, &actorLabel); err != nil || actorKind != "SYSTEM" || actorUserID != nil ||
		actorLabel != "startup-test-bootstrap" {
		t.Fatalf("bootstrap actor = %s/%v/%s, error=%v", actorKind, actorUserID, actorLabel, err)
	}
	loggedIn, err := fixture.service.Login(context.Background(), "test", "test")
	if err != nil || loggedIn.User.Username != "test" || loggedIn.User.Role != "ADMIN" {
		t.Fatalf("test login = %#v, %v", loggedIn.User, err)
	}
	release, err := New(
		context.Background(), fixture.database.SQL, fixture.credentials, config.ModeRelease,
		authn.EmptyBlocklist{}, func() time.Time { return *fixture.now },
	)
	if err != nil {
		t.Fatal(err)
	}
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
	if err := fixture.database.SQL.QueryRow("SELECT count(*) FROM users").Scan(&users); err != nil || users != 0 {
		t.Fatalf("users after invalid setup = %d, %v", users, err)
	}
	initialized, err := fixture.service.Initialize(context.Background(), InitializeRequest{
		SetupCode: fixture.credentials.SetupCode(), Username: "admin", DisplayName: "Administrator",
		Password: "a sufficiently long phrase", PasswordConfirmation: "a sufficiently long phrase",
	})
	if err != nil {
		t.Fatal(err)
	}
	if initialized.User.Role != "ADMIN" || initialized.CSRFToken == "" || initialized.CookieToken == "" {
		t.Fatalf("initialized session = %#v", initialized)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	authenticated, err := fixture.service.Authenticate(context.Background(), loggedIn.CookieToken)
	if err != nil || authenticated.Principal.UserID != loggedIn.Principal.UserID ||
		!MatchesCSRF(loggedIn.CookieToken, loggedIn.CSRFToken) || MatchesCSRF(loggedIn.CookieToken, "wrong") {
		t.Fatalf("authenticated = %#v, %v", authenticated.Principal, err)
	}
	rotated, err := fixture.service.ChangePassword(
		context.Background(), loggedIn.Principal, "a sufficiently long phrase",
		"a new sufficiently long phrase", "a new sufficiently long phrase",
	)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.CookieToken == loggedIn.CookieToken {
		t.Fatal("password change reused the old session token")
	}
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

func TestStartRejectsCorruptCredentialWithoutComputingIt(t *testing.T) {
	t.Parallel()
	fixture := newAccountFixture(t, config.ModeTest)
	if err := fixture.service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.SQL.Exec(`
UPDATE user_credentials SET password_hash='$argon2id$v=19$m=999999,t=2,p=1$bad$bad'
`); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.Start(context.Background()); !errors.Is(err, authn.ErrCredential) {
		t.Fatalf("corrupt credential Start() = %v", err)
	}
}
