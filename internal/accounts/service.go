package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	accountpersistence "retrom/internal/persistence/accounts"
	accountservice "retrom/internal/service/accounts"

	"retrom/internal/dbexec"

	"retrom/internal/persistence/recordstore"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/config"
	retromruntime "retrom/internal/runtime"
)

var (
	ErrAuthentication       = accountservice.ErrAuthentication
	ErrAuthenticationNeeded = accountservice.ErrAuthenticationNeeded
	ErrInitialization       = errors.New("INITIALIZATION_REQUIRED")
	ErrInitializationDone   = errors.New("INITIALIZATION_ALREADY_COMPLETED")
	ErrInitializationProof  = errors.New("INITIALIZATION_PROOF_INVALID")
	ErrInitializationState  = errors.New("INITIALIZATION_STATE_INVALID")
	ErrTestCredential       = errors.New("TEST_DEFAULT_CREDENTIAL_ACTIVE")
	errIdentityGeneration   = errors.New("generate account identity")
	errSessionGeneration    = errors.New("generate session id")
)

type Service struct {
	limiter     *accountservice.Limiter
	database    *sql.DB
	credentials *retromruntime.Credentials
	hasher      *authn.PasswordHasher
	blocklist   authn.Blocklist
	mode        config.Mode
	now         func() time.Time
	random      io.Reader
	dummyPHC    string
}

type (
	User    = accountservice.User
	Session = accountservice.Session
)

type Context struct {
	InstanceState            string
	Mode                     config.Mode
	Session                  *Session
	TestDefaultAccountActive bool
}

type InitializeRequest struct {
	SetupCode            string
	Username             string
	DisplayName          string
	Password             string
	PasswordConfirmation string
}

func New(
	ctx context.Context,
	database *sql.DB,
	credentials *retromruntime.Credentials,
	mode config.Mode,
	blocklist authn.Blocklist,
	now func() time.Time,
) (*Service, error) {
	hasher := authn.NewPasswordHasher()
	dummy, err := hasher.Hash(ctx, "retrom dummy credential")
	if err != nil {
		return nil, fmt.Errorf("prepare dummy credential: %w", err)
	}
	return &Service{
		limiter:  accountservice.NewLimiter(accountpersistence.NewRateLimits(database), credentials, now),
		database: database, credentials: credentials, hasher: hasher, blocklist: blocklist,
		mode: mode, now: now, random: rand.Reader, dummyPHC: dummy,
	}, nil
}

func (service *Service) Start(ctx context.Context) error {
	state, testDefault, err := service.instanceState(ctx)
	if err != nil {
		return err
	}
	if state == "PENDING" {
		var users, profiles int
		if err := service.database.QueryRowContext(ctx, `
SELECT (SELECT count(*) FROM users),(SELECT count(*) FROM profiles)
`).Scan(&users, &profiles); err != nil {
			return fmt.Errorf("read initialization counts: %w", err)
		}
		if users != 0 || profiles != 0 {
			return ErrInitializationState
		}
		if service.mode == config.ModeTest {
			_, err = service.bootstrap(ctx, "test", "test", "test", "TEST_DEFAULT")
			return err
		}
		return nil
	}
	if state != "COMPLETED" {
		return ErrInitializationState
	}
	var admins, orphanProfiles int
	if err := service.database.QueryRowContext(ctx, `
SELECT
  (SELECT count(*) FROM users WHERE role='ADMIN' AND status='ENABLED'),
  (SELECT count(*) FROM profiles profile
   LEFT JOIN users user ON user.profile_id=profile.id
   WHERE user.id IS NULL)
`).Scan(&admins, &orphanProfiles); err != nil {
		return fmt.Errorf("read completed initialization invariants: %w", err)
	}
	if admins == 0 || orphanProfiles != 0 {
		return ErrInitializationState
	}
	if service.mode == config.ModeRelease && testDefault {
		return ErrTestCredential
	}
	return service.validateCredentialStore(ctx)
}

func (service *Service) validateCredentialStore(ctx context.Context) error {
	rows, err := service.database.QueryContext(ctx, `
SELECT password_scheme,password_hash FROM user_credentials ORDER BY user_id
`)
	if err != nil {
		return fmt.Errorf("read credential store: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var scheme, encoded string
		if err := rows.Scan(&scheme, &encoded); err != nil || scheme != "ARGON2ID_V1" || authn.ValidatePHC(encoded) != nil {
			return authn.ErrCredential
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan credential store: %w", err)
	}
	return nil
}

func (service *Service) Initialize(ctx context.Context, request InitializeRequest) (Session, error) {
	if service.mode != config.ModeRelease {
		return Session{}, ErrInitializationDone
	}
	if !service.credentials.MatchesSetupCode(request.SetupCode) {
		return Session{}, ErrInitializationProof
	}
	username, err := authn.NormalizeUsername(request.Username)
	if err != nil {
		return Session{}, fmt.Errorf("normalize initial username: %w", err)
	}
	displayName, err := authn.NormalizeDisplayName(request.DisplayName)
	if err != nil {
		return Session{}, fmt.Errorf("normalize initial display name: %w", err)
	}
	password, err := authn.ValidatePassword(
		request.Password, request.PasswordConfirmation, username, displayName, service.blocklist,
	)
	if err != nil {
		return Session{}, fmt.Errorf("validate initial password: %w", err)
	}
	return service.bootstrap(ctx, username, displayName, password, "RELEASE_SETUP")
}

// Initialization invariants and atomic writes remain auditable in one transaction.
func (service *Service) bootstrap(
	ctx context.Context,
	username, displayName, password, kind string,
) (Session, error) {
	input, err := service.prepareBootstrap(ctx, username, displayName, password)
	if err != nil {
		return Session{}, err
	}
	now := service.now().UTC().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, fmt.Errorf("begin initialization: %w", err)
	}
	defer dbexec.Rollback(transaction)
	return persistBootstrap(ctx, transaction, input, kind, now)
}

type bootstrapInput struct {
	username, displayName, encodedPassword string
	session                                preparedSession
	userID, profileID                      string
}

func (service *Service) prepareBootstrap(
	ctx context.Context,
	username, displayName, password string,
) (bootstrapInput, error) {
	encoded, err := service.hasher.Hash(ctx, password)
	if err != nil {
		return bootstrapInput{}, fmt.Errorf("hash initial password: %w", err)
	}
	prepared, err := service.prepareSession()
	if err != nil {
		return bootstrapInput{}, err
	}
	userID, profileID := newID(), newID()
	if userID == "" || profileID == "" {
		return bootstrapInput{}, errIdentityGeneration
	}
	return bootstrapInput{
		username: username, displayName: displayName, encodedPassword: encoded,
		session: prepared, userID: userID, profileID: profileID,
	}, nil
}

func persistBootstrap(
	ctx context.Context,
	transaction *sql.Tx,
	input bootstrapInput,
	kind string,
	now int64,
) (Session, error) {
	var state string
	var users, profiles int
	if err := transaction.QueryRowContext(ctx, `
SELECT state,(SELECT count(*) FROM users),(SELECT count(*) FROM profiles)
FROM instance_state WHERE id=1
`).Scan(&state, &users, &profiles); err != nil {
		return Session{}, fmt.Errorf("read initialization state: %w", err)
	}
	if state != "PENDING" {
		return Session{}, ErrInitializationDone
	}
	if users != 0 || profiles != 0 {
		return Session{}, ErrInitializationState
	}
	if _, err := recordstore.CreateProfiles(ctx, transaction, `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,?,?)
`, input.profileID, input.displayName, now); err != nil {
		return Session{}, fmt.Errorf("create profile: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,'ADMIN','ENABLED',?,?)
`, input.userID, input.profileID, input.username, input.displayName, now, now); err != nil {
		return Session{}, fmt.Errorf("create initial user: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO user_credentials(user_id,password_hash,password_scheme,password_changed_at_ms,created_at_ms)
VALUES(?,?,'ARGON2ID_V1',?,?)
`, input.userID, input.encodedPassword, now, now); err != nil {
		return Session{}, fmt.Errorf("create initial credential: %w", err)
	}
	testDefault := 0
	actorLabel := "release-setup"
	if kind == "TEST_DEFAULT" {
		testDefault = 1
		actorLabel = "startup-test-bootstrap"
	}
	result, err := recordstore.UpdateInstanceState(ctx, transaction, recordstore.Update{
		Set: `
state='COMPLETED',bootstrap_kind=?,initial_admin_user_id=?,
test_default_password_active=?,version=version+1,updated_at_ms=?,initialized_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=1 AND state='PENDING'`,
		},
		Values: []any{kind, input.userID, testDefault, now, now},
	})
	if err != nil {
		return Session{}, fmt.Errorf("complete initialization: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return Session{}, ErrInitializationDone
	}
	if err := insertPreparedSession(ctx, transaction, input.session, input.userID, 1, now); err != nil {
		return Session{}, err
	}
	auditID := newID()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms)
VALUES(?,'SYSTEM',NULL,?,'INSTANCE_INITIALIZED','USER',?,NULL,'{}','{}',NULL,?)
`, auditID, actorLabel, input.userID, now); err != nil {
		return Session{}, fmt.Errorf("audit initialization: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Session{}, fmt.Errorf("commit initialization: %w", err)
	}
	return input.session.view(
		User{UserID: input.userID, Username: input.username, DisplayName: input.displayName, Role: "ADMIN"},
		input.profileID,
		1,
		now,
	), nil
}

func (service *Service) authentication() *accountservice.Authentication {
	return accountservice.NewAuthentication(
		accountpersistence.NewAuthentication(
			service.database,
		),
		service.hasher,
		func() (accountservice.SessionMaterial, error) {
			prepared, err := service.prepareSession()
			if err != nil {
				return accountservice.SessionMaterial{}, err
			}
			return accountservice.SessionMaterial{ID: prepared.id, Token: prepared.token, Hash: prepared.hash}, nil
		},
		service.dummyPHC,
		func() time.Time { return service.now() },
	)
}

func (service *Service) Login(ctx context.Context, username, password string) (Session, error) {
	result, err := service.authentication().Login(ctx, username, password)
	if err != nil {
		return Session{}, fmt.Errorf("authenticate login: %w", err)
	}
	return result, nil
}

func (service *Service) Authenticate(ctx context.Context, token string) (Session, error) {
	result, err := service.authentication().Authenticate(ctx, token)
	if err != nil {
		return Session{}, fmt.Errorf("authenticate session: %w", err)
	}
	return result, nil
}

func (service *Service) Logout(ctx context.Context, id string) error {
	if err := service.authentication().Logout(ctx, id); err != nil {
		return fmt.Errorf("logout session: %w", err)
	}
	return nil
}

// Password rotation, revocation, and replacement session issuance must remain atomic.
func (service *Service) ChangePassword(
	ctx context.Context,
	principal authn.Principal,
	currentPassword, newPassword, confirmation string,
) (Session, error) {
	input, err := service.preparePasswordChange(ctx, principal, currentPassword, newPassword, confirmation)
	if err != nil {
		return Session{}, err
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, fmt.Errorf("begin password change: %w", err)
	}
	defer dbexec.Rollback(transaction)
	return persistPasswordChange(ctx, transaction, principal, input)
}

type passwordChangeInput struct {
	newHash string
	session preparedSession
	now     int64
}

func (service *Service) preparePasswordChange(
	ctx context.Context,
	principal authn.Principal,
	currentPassword, newPassword, confirmation string,
) (passwordChangeInput, error) {
	current, err := authn.NormalizeLoginPassword(currentPassword)
	if err != nil {
		return passwordChangeInput{}, ErrAuthentication
	}
	var encoded string
	if err := service.database.QueryRowContext(ctx, `
SELECT password_hash FROM user_credentials WHERE user_id=?
`, principal.UserID).Scan(&encoded); err != nil {
		return passwordChangeInput{}, ErrAuthentication
	}
	ok, err := service.hasher.Verify(ctx, current, encoded)
	if err != nil || !ok {
		return passwordChangeInput{}, ErrAuthentication
	}
	normalized, err := authn.ValidatePassword(
		newPassword, confirmation, principal.Username, principal.DisplayName, service.blocklist,
	)
	if err != nil {
		return passwordChangeInput{}, fmt.Errorf("validate replacement password: %w", err)
	}
	newHash, err := service.hasher.Hash(ctx, normalized)
	if err != nil {
		return passwordChangeInput{}, fmt.Errorf("hash replacement password: %w", err)
	}
	prepared, err := service.prepareSession()
	if err != nil {
		return passwordChangeInput{}, err
	}
	return passwordChangeInput{newHash: newHash, session: prepared, now: service.now().UTC().UnixMilli()}, nil
}

func persistPasswordChange(
	ctx context.Context,
	transaction *sql.Tx,
	principal authn.Principal,
	input passwordChangeInput,
) (Session, error) {
	var version int64
	var status, role string
	if err := transaction.QueryRowContext(ctx, `
UPDATE users SET session_version=session_version+1,version=version+1,updated_at_ms=?
WHERE id=? AND status='ENABLED' RETURNING session_version,status,role
`, input.now, principal.UserID).Scan(&version, &status, &role); err != nil {
		return Session{}, ErrAuthenticationNeeded
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE user_credentials SET password_hash=?,password_changed_at_ms=? WHERE user_id=?
	`, input.newHash, input.now, principal.UserID); err != nil {
		return Session{}, fmt.Errorf("replace password credential: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE auth_sessions SET revoked_at_ms=?,revoked_reason='PASSWORD_CHANGED'
WHERE user_id=? AND revoked_at_ms IS NULL
	`, input.now, principal.UserID); err != nil {
		return Session{}, fmt.Errorf("revoke password-change sessions: %w", err)
	}
	if _, err := recordstore.UpdateAccountLinks(ctx, transaction, recordstore.Update{
		Set: `revoked_at_ms=?,revoked_by_kind='SYSTEM',version=version+1`,
		Scope: recordstore.Scope{
			Where: `
kind='PASSWORD_RESET'
AND target_user_id=?
AND consumed_at_ms IS NULL
AND revoked_at_ms IS NULL
AND expires_at_ms>?
`,
			Args: []any{principal.UserID, input.now},
		},
		Values: []any{input.now},
	}); err != nil {
		return Session{}, fmt.Errorf("revoke password-reset links: %w", err)
	}
	if principal.Username == "test" {
		_, _ = recordstore.UpdateInstanceState(ctx, transaction, recordstore.Update{
			Set: `test_default_password_active=0,version=version+1,updated_at_ms=?`,
			Scope: recordstore.Scope{
				Where: `id=1 AND test_default_password_active=1`,
			},
			Values: []any{input.now},
		})
	}
	if err := insertPreparedSession(ctx, transaction, input.session, principal.UserID, version, input.now); err != nil {
		return Session{}, err
	}
	if err := insertUserAudit(
		ctx, transaction, principal, "PASSWORD_CHANGED", "USER", principal.UserID,
		map[string]any{"sessionVersion": version - 1}, map[string]any{"sessionVersion": version}, input.now,
	); err != nil {
		return Session{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Session{}, fmt.Errorf("commit password change: %w", err)
	}
	return input.session.view(User{
		UserID: principal.UserID, Username: principal.Username, DisplayName: principal.DisplayName, Role: role,
	}, principal.ProfileID, version, input.now), nil
}

func (service *Service) Context(ctx context.Context, cookie string) (Context, error) {
	state, testDefault, err := service.instanceState(ctx)
	if err != nil {
		return Context{}, err
	}
	result := Context{Mode: service.mode, TestDefaultAccountActive: service.mode == config.ModeTest && testDefault}
	if state == "PENDING" {
		result.InstanceState = "INITIALIZATION_REQUIRED"
		return result, nil
	}
	result.InstanceState = "READY"
	if cookie == "" {
		return result, nil
	}
	session, err := service.Authenticate(ctx, cookie)
	if err == nil {
		result.Session = &session
	} else if !errors.Is(err, ErrAuthenticationNeeded) {
		return Context{}, fmt.Errorf("read authentication context: %w", err)
	}
	return result, nil
}

func (service *Service) instanceState(ctx context.Context) (string, bool, error) {
	var state string
	var testDefault int
	if err := service.database.QueryRowContext(ctx, `
SELECT state,test_default_password_active FROM instance_state WHERE id=1
`).Scan(&state, &testDefault); err != nil {
		return "", false, fmt.Errorf("read instance state: %w", err)
	}
	return state, testDefault == 1, nil
}

type preparedSession struct {
	id, token string
	hash      [32]byte
}

func (service *Service) prepareSession() (preparedSession, error) {
	id := newID()
	if id == "" {
		return preparedSession{}, errSessionGeneration
	}
	raw := make([]byte, 32)
	if _, err := io.ReadFull(service.random, raw); err != nil {
		return preparedSession{}, fmt.Errorf("generate session token: %w", err)
	}
	return preparedSession{
		id: id, token: base64.RawURLEncoding.EncodeToString(raw), hash: sha256.Sum256(raw),
	}, nil
}

func insertPreparedSession(
	ctx context.Context,
	transaction *sql.Tx,
	prepared preparedSession,
	userID string,
	sessionVersion, now int64,
) error {
	record := (accountservice.SessionMaterial{ID: prepared.id, Hash: prepared.hash}).Record(userID, sessionVersion, now)
	_, err := transaction.ExecContext(ctx, `
INSERT INTO auth_sessions(id,user_id,token_sha256,user_session_version,created_at_ms,last_seen_at_ms,
idle_expires_at_ms,absolute_expires_at_ms)
VALUES(?,?,?,?,?,?,?,?)
`, prepared.id, userID, prepared.hash[:], sessionVersion, now, now,
		record.IdleExpiry, record.AbsoluteExpiry)
	if err != nil {
		return fmt.Errorf("create auth session: %w", err)
	}
	return nil
}

func (prepared preparedSession) view(user User, profileID string, version, now int64) Session {
	return (accountservice.SessionMaterial{
		ID:    prepared.id,
		Token: prepared.token,
		Hash:  prepared.hash,
	}).View(
		user,
		profileID,
		version,
		now,
	)
}

func MatchesCSRF(sessionToken, supplied string) bool {
	return accountservice.MatchesCSRF(sessionToken, supplied)
}

func newID() string {
	value, err := uuid.NewV7()
	if err != nil {
		return ""
	}
	return value.String()
}
