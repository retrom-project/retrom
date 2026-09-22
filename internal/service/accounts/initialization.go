package accounts

import (
	"context"
	"errors"
	"fmt"

	"retrom/internal/authn"
	"retrom/internal/config"
)

var (
	ErrInitialization      = errors.New("INITIALIZATION_REQUIRED")
	ErrInitializationDone  = errors.New("INITIALIZATION_ALREADY_COMPLETED")
	ErrInitializationState = errors.New("INITIALIZATION_STATE_INVALID")
	ErrTestCredential      = errors.New("TEST_DEFAULT_CREDENTIAL_ACTIVE")
)

type InitializationService struct {
	repository InitializationRepository
	options    InitializationOptions
}

func NewInitialization(repository InitializationRepository, options InitializationOptions) *InitializationService {
	return &InitializationService{repository: repository, options: options}
}

func (service *InitializationService) State(ctx context.Context) (InitializationState, error) {
	state, err := service.repository.State(ctx)
	if err != nil {
		return state, fmt.Errorf("read instance initialization: %w", err)
	}
	return state, nil
}

func (service *InitializationService) Start(ctx context.Context) error {
	state, err := service.State(ctx)
	if err != nil {
		return err
	}
	if state.State == "PENDING" {
		if state.Users != 0 || state.Profiles != 0 {
			return ErrInitializationState
		}
		if service.options.Mode == config.ModeTest {
			_, err := service.bootstrap(ctx, "test", "test", "test", "TEST_DEFAULT")
			return err
		}
		return nil
	}
	if state.State != "COMPLETED" || state.EnabledAdmins == 0 || state.OrphanProfiles != 0 {
		return ErrInitializationState
	}
	if service.options.Mode == config.ModeRelease && state.TestDefault {
		return ErrTestCredential
	}
	return service.validateCredentials(ctx)
}

func (service *InitializationService) validateCredentials(ctx context.Context) error {
	credentials, err := service.repository.Credentials(ctx)
	if err != nil {
		return fmt.Errorf("read initialized credential store: %w", err)
	}
	for _, credential := range credentials {
		if credential.Missing || credential.Scheme != "ARGON2ID_V1" || authn.ValidatePHC(credential.Hash) != nil {
			return authn.ErrCredential
		}
	}
	return nil
}

func (service *InitializationService) Initialize(ctx context.Context, request InitializeRequest) (Session, error) {
	if service.options.Mode != config.ModeRelease {
		return Session{}, ErrInitializationDone
	}
	username, err := authn.NormalizeUsername(request.Username)
	if err != nil {
		return Session{}, fmt.Errorf("normalize initial username: %w", err)
	}
	display, err := authn.NormalizeDisplayName(request.DisplayName)
	if err != nil {
		return Session{}, fmt.Errorf("normalize initial display name: %w", err)
	}
	password, err := authn.ValidatePassword(
		request.Password,
		request.PasswordConfirmation,
		username,
		display,
		service.options.Blocklist,
	)
	if err != nil {
		return Session{}, fmt.Errorf("validate initial password: %w", err)
	}
	return service.bootstrap(ctx, username, display, password, "RELEASE_SETUP")
}
