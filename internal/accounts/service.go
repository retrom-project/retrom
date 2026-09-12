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

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/config"
	retromruntime "retrom/internal/runtime"
)

var (
	ErrAuthentication       = accountservice.ErrAuthentication
	ErrAuthenticationNeeded = accountservice.ErrAuthenticationNeeded
	ErrInitialization       = accountservice.ErrInitialization
	ErrInitializationDone   = accountservice.ErrInitializationDone
	ErrInitializationProof  = accountservice.ErrInitializationProof
	ErrInitializationState  = accountservice.ErrInitializationState
	ErrTestCredential       = accountservice.ErrTestCredential
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

type InitializeRequest = accountservice.InitializeRequest

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

func (service *Service) initialization() *accountservice.InitializationService {
	return accountservice.NewInitialization(
		accountpersistence.NewInitialization(
			service.database,
		),
		accountservice.InitializationOptions{
			Mode:        service.mode,
			Credentials: service.credentials,
			Hasher:      service.hasher,
			Blocklist:   service.blocklist,
			Mint:        service.mintSession,
			Now: func() time.Time {
				return service.now()
			},
		},
	)
}

func (service *Service) Start(ctx context.Context) error {
	if err := service.initialization().Start(ctx); err != nil {
		return fmt.Errorf("start account service: %w", err)
	}
	return nil
}

func (service *Service) Initialize(ctx context.Context, request InitializeRequest) (Session, error) {
	result, err := service.initialization().Initialize(ctx, request)
	if err != nil {
		return Session{}, fmt.Errorf("initialize account service: %w", err)
	}
	return result, nil
}

func (service *Service) mintSession() (accountservice.SessionMaterial, error) {
	prepared, err := service.prepareSession()
	if err != nil {
		return accountservice.SessionMaterial{}, err
	}
	return accountservice.SessionMaterial{ID: prepared.id, Token: prepared.token, Hash: prepared.hash}, nil
}

func (service *Service) authentication() *accountservice.Authentication {
	return accountservice.NewAuthentication(
		accountpersistence.NewAuthentication(
			service.database,
		),
		service.hasher,
		service.mintSession,
		service.dummyPHC,
		func() time.Time {
			return service.now()
		},
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

func (service *Service) ChangePassword(
	ctx context.Context,
	principal authn.Principal,
	current, password, confirmation string,
) (Session, error) {
	passwords := accountservice.NewPasswords(
		accountpersistence.NewPasswords(
			service.database,
		),
		service.hasher,
		service.blocklist,
		service.mintSession,
		func() time.Time {
			return service.now()
		},
	)
	result, err := passwords.Change(
		ctx,
		accountservice.PasswordActor{
			UserID:         principal.UserID,
			SessionID:      principal.SessionID,
			SessionVersion: principal.SessionVersion,
		},
		current,
		password,
		confirmation,
	)
	if err != nil {
		return Session{}, fmt.Errorf("change account password: %w", err)
	}
	return result, nil
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
	state, err := service.initialization().State(ctx)
	if err != nil {
		return "", false, fmt.Errorf("read instance state: %w", err)
	}
	return state.State, state.TestDefault, nil
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
