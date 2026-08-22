package accounts

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/config"
	retromruntime "retrom/internal/runtime"
)

const (
	idleDuration     = 8 * time.Hour
	absoluteDuration = 24 * time.Hour
	refreshInterval  = 5 * time.Minute
)

var (
	ErrAuthentication       = errors.New("AUTHENTICATION_FAILED")
	ErrAuthenticationNeeded = errors.New("AUTHENTICATION_REQUIRED")
	ErrInitialization       = errors.New("INITIALIZATION_REQUIRED")
	ErrInitializationDone   = errors.New("INITIALIZATION_ALREADY_COMPLETED")
	ErrInitializationProof  = errors.New("INITIALIZATION_PROOF_INVALID")
	ErrInitializationState  = errors.New("INITIALIZATION_STATE_INVALID")
	ErrTestCredential       = errors.New("TEST_DEFAULT_CREDENTIAL_ACTIVE")
	errIdentityGeneration   = errors.New("generate account identity")
	errSessionGeneration    = errors.New("generate session id")
)

type Service struct {
	database    *sql.DB
	credentials *retromruntime.Credentials
	hasher      *authn.PasswordHasher
	blocklist   authn.Blocklist
	mode        config.Mode
	now         func() time.Time
	random      io.Reader
	dummyPHC    string
}

type User struct {
	UserID      string `json:"userId"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
}

type Session struct {
	Principal           authn.Principal
	User                User
	CSRFToken           string
	IdleExpiresAtMS     int64
	AbsoluteExpiresAtMS int64
	CookieToken         string
}

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
	defer cleanup.Rollback(transaction)
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
	if _, err := transaction.ExecContext(ctx, `
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
	result, err := transaction.ExecContext(ctx, `
UPDATE instance_state SET state='COMPLETED',bootstrap_kind=?,initial_admin_user_id=?,
test_default_password_active=?,version=version+1,updated_at_ms=?,initialized_at_ms=?
WHERE id=1 AND state='PENDING'
`, kind, input.userID, testDefault, now, now)
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

// Authentication deliberately keeps the dummy-hash and real-user paths indistinguishable.
func (service *Service) Login(ctx context.Context, usernameInput, passwordInput string) (Session, error) {
	identity, err := service.verifyLogin(ctx, usernameInput, passwordInput)
	if err != nil {
		return Session{}, err
	}
	prepared, err := service.prepareSession()
	if err != nil {
		return Session{}, err
	}
	now := service.now().UTC().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, fmt.Errorf("begin login: %w", err)
	}
	defer cleanup.Rollback(transaction)
	result, err := transaction.ExecContext(ctx, `
UPDATE users SET last_login_at_ms=?,updated_at_ms=?
WHERE id=? AND status='ENABLED' AND session_version=?
`, now, now, identity.userID, identity.sessionVersion)
	if err != nil {
		return Session{}, fmt.Errorf("record login: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return Session{}, ErrAuthentication
	}
	if err := insertPreparedSession(
		ctx, transaction, prepared, identity.userID, identity.sessionVersion, now,
	); err != nil {
		return Session{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Session{}, fmt.Errorf("commit login: %w", err)
	}
	return prepared.view(
		User{
			UserID: identity.userID, Username: identity.username,
			DisplayName: identity.displayName, Role: identity.role,
		},
		identity.profileID, identity.sessionVersion, now,
	), nil
}

type loginIdentity struct {
	userID, profileID, username, displayName, role string
	sessionVersion                                 int64
}

func (service *Service) verifyLogin(
	ctx context.Context,
	usernameInput, passwordInput string,
) (loginIdentity, error) {
	username, usernameErr := authn.NormalizeUsername(usernameInput)
	password, passwordErr := authn.NormalizeLoginPassword(passwordInput)
	var userID, profileID, displayName, role, status, encoded string
	var sessionVersion int64
	lookupErr := sql.ErrNoRows
	if usernameErr == nil {
		lookupErr = service.database.QueryRowContext(ctx, `
SELECT u.id,u.profile_id,u.display_name,u.role,u.status,u.session_version,c.password_hash
FROM users u JOIN user_credentials c ON c.user_id=u.id WHERE u.username=?
`, username).Scan(&userID, &profileID, &displayName, &role, &status, &sessionVersion, &encoded)
	}
	if lookupErr != nil {
		encoded = service.dummyPHC
	}
	if passwordErr != nil {
		password = strings.Repeat("x", 24)
	}
	verified, verifyErr := service.hasher.Verify(ctx, password, encoded)
	if verifyErr != nil {
		return loginIdentity{}, fmt.Errorf("verify login password: %w", verifyErr)
	}
	if usernameErr != nil || passwordErr != nil || lookupErr != nil || !verified || status != "ENABLED" {
		return loginIdentity{}, ErrAuthentication
	}
	return loginIdentity{
		userID: userID, profileID: profileID, username: username,
		displayName: displayName, role: role, sessionVersion: sessionVersion,
	}, nil
}

func (service *Service) Authenticate(ctx context.Context, token string) (Session, error) {
	raw, err := decodeToken(token)
	if err != nil {
		return Session{}, ErrAuthenticationNeeded
	}
	digest := sha256.Sum256(raw)
	var sessionID, userID, profileID, username, displayName, role, status string
	var userVersion, sessionVersion, lastSeen, idleExpiry, absoluteExpiry int64
	var revoked sql.NullInt64
	err = service.database.QueryRowContext(ctx, `
SELECT s.id,u.id,u.profile_id,u.username,u.display_name,u.role,u.status,u.session_version,
s.user_session_version,s.last_seen_at_ms,s.idle_expires_at_ms,s.absolute_expires_at_ms,s.revoked_at_ms
FROM auth_sessions s JOIN users u ON u.id=s.user_id WHERE s.token_sha256=?
`, digest[:]).Scan(
		&sessionID, &userID, &profileID, &username, &displayName, &role, &status, &userVersion,
		&sessionVersion, &lastSeen, &idleExpiry, &absoluteExpiry, &revoked,
	)
	now := service.now().UTC().UnixMilli()
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrAuthenticationNeeded
	}
	if err != nil {
		return Session{}, fmt.Errorf("authenticate session: %w", err)
	}
	if revoked.Valid || status != "ENABLED" || userVersion != sessionVersion ||
		now >= idleExpiry || now >= absoluteExpiry {
		return Session{}, ErrAuthenticationNeeded
	}
	if now-lastSeen >= refreshInterval.Milliseconds() {
		newIdle := min(now+idleDuration.Milliseconds(), absoluteExpiry)
		_, _ = service.database.ExecContext(ctx, `
UPDATE auth_sessions SET last_seen_at_ms=?,idle_expires_at_ms=?
WHERE id=? AND revoked_at_ms IS NULL AND last_seen_at_ms=?
`, now, newIdle, sessionID, lastSeen)
		idleExpiry = newIdle
	}
	principal := authn.Principal{
		UserID: userID, ProfileID: profileID, Username: username, DisplayName: displayName,
		Role: role, SessionID: sessionID, SessionVersion: sessionVersion, SessionToken: token,
	}
	return Session{
		Principal: principal, User: User{UserID: userID, Username: username, DisplayName: displayName, Role: role},
		CSRFToken: csrfToken(raw), IdleExpiresAtMS: idleExpiry, AbsoluteExpiresAtMS: absoluteExpiry,
		CookieToken: token,
	}, nil
}

func (service *Service) Logout(ctx context.Context, sessionID string) error {
	now := service.now().UTC().UnixMilli()
	_, err := service.database.ExecContext(ctx, `
UPDATE auth_sessions SET revoked_at_ms=?,revoked_reason='LOGOUT'
WHERE id=? AND revoked_at_ms IS NULL
`, now, sessionID)
	if err != nil {
		return fmt.Errorf("revoke auth session: %w", err)
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
	defer cleanup.Rollback(transaction)
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
	if _, err := transaction.ExecContext(ctx, `
UPDATE account_links SET revoked_at_ms=?,revoked_by_kind='SYSTEM',version=version+1
WHERE kind='PASSWORD_RESET'
AND target_user_id=?
AND consumed_at_ms IS NULL
AND revoked_at_ms IS NULL
AND expires_at_ms>?
`, input.now, principal.UserID, input.now); err != nil {
		return Session{}, fmt.Errorf("revoke password-reset links: %w", err)
	}
	if principal.Username == "test" {
		_, _ = transaction.ExecContext(ctx, `
UPDATE instance_state SET test_default_password_active=0,version=version+1,updated_at_ms=?
WHERE id=1 AND test_default_password_active=1
`, input.now)
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
	_, err := transaction.ExecContext(ctx, `
INSERT INTO auth_sessions(id,user_id,token_sha256,user_session_version,created_at_ms,last_seen_at_ms,
idle_expires_at_ms,absolute_expires_at_ms)
VALUES(?,?,?,?,?,?,?,?)
`, prepared.id, userID, prepared.hash[:], sessionVersion, now, now,
		now+idleDuration.Milliseconds(), now+absoluteDuration.Milliseconds())
	if err != nil {
		return fmt.Errorf("create auth session: %w", err)
	}
	return nil
}

func (prepared preparedSession) view(user User, profileID string, version, now int64) Session {
	raw, _ := base64.RawURLEncoding.DecodeString(prepared.token)
	principal := authn.Principal{
		UserID: user.UserID, ProfileID: profileID, Username: user.Username, DisplayName: user.DisplayName,
		Role: user.Role, SessionID: prepared.id, SessionVersion: version, SessionToken: prepared.token,
	}
	return Session{
		Principal: principal, User: user, CSRFToken: csrfToken(raw),
		IdleExpiresAtMS:     now + idleDuration.Milliseconds(),
		AbsoluteExpiresAtMS: now + absoluteDuration.Milliseconds(), CookieToken: prepared.token,
	}
}

func csrfToken(raw []byte) string {
	mac := hmac.New(sha256.New, raw)
	_, _ = mac.Write([]byte("retrom-csrf-v1"))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func MatchesCSRF(sessionToken, supplied string) bool {
	raw, err := decodeToken(sessionToken)
	if err != nil {
		return false
	}
	expected := csrfToken(raw)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(supplied)) == 1
}

func decodeToken(encoded string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != encoded {
		return nil, ErrAuthenticationNeeded
	}
	return raw, nil
}

func newID() string {
	value, err := uuid.NewV7()
	if err != nil {
		return ""
	}
	return value.String()
}
