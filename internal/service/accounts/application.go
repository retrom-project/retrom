package accounts

import (
	"context"
	"errors"
	"fmt"

	"retrom/internal/authn"
	"retrom/internal/config"
)

type Modules struct {
	Initialization *InitializationService
	Authentication *Authentication
	Passwords      *PasswordService
	Recovery       *RecoveryService
	Directory      *DirectoryService
	Administration *AdministrationService
	Links          *LinkService
	Issuance       *LinkIssuanceService
	Consumption    *LinkConsumptionService
	Limiter        *Limiter
}
type Service struct {
	modules Modules
	mode    config.Mode
}
type Context struct {
	InstanceState            string
	Mode                     config.Mode
	Session                  *Session
	TestDefaultAccountActive bool
}

func New(modules Modules, mode config.Mode) *Service { return &Service{modules: modules, mode: mode} }

func (service *Service) Start(ctx context.Context) error {
	return service.modules.Initialization.Start(ctx)
}

func (service *Service) Initialize(ctx context.Context, request InitializeRequest) (Session, error) {
	return service.modules.Initialization.Initialize(ctx, request)
}

func (service *Service) Login(ctx context.Context, username, password string) (Session, error) {
	return service.modules.Authentication.Login(ctx, username, password)
}

func (service *Service) Authenticate(ctx context.Context, token string) (Session, error) {
	result, err := service.modules.Authentication.Authenticate(ctx, token)
	if err != nil {
		return Session{}, fmt.Errorf("authenticate session: %w", err)
	}
	return result, nil
}

func (service *Service) Logout(ctx context.Context, id string) error {
	return service.modules.Authentication.Logout(ctx, id)
}

func (service *Service) ChangePassword(
	ctx context.Context,
	principal authn.Principal,
	current, password, confirmation string,
) (Session, error) {
	return service.modules.Passwords.Change(
		ctx,
		PasswordActor{
			UserID:         principal.UserID,
			SessionID:      principal.SessionID,
			SessionVersion: principal.SessionVersion,
		},
		current,
		password,
		confirmation,
	)
}

func (service *Service) Context(ctx context.Context, cookie string) (Context, error) {
	state, err := service.modules.Initialization.State(ctx)
	if err != nil {
		return Context{}, fmt.Errorf("read instance state: %w", err)
	}
	result := Context{Mode: service.mode, TestDefaultAccountActive: service.mode == config.ModeTest && state.TestDefault}
	if state.State == "PENDING" {
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

func (service *Service) OfflineAdminReset(ctx context.Context, username, password, confirmation string) error {
	return service.modules.Recovery.Reset(ctx, username, password, confirmation)
}
