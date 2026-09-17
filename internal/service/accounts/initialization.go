package accounts

import (
	"context"
	"fmt"

	model "retrom/internal/model/accounts"

	"retrom/internal/capability/security/authn"
)

type InitializationService struct {
	repository model.InitializationRepository
	options    model.InitializationOptions
}

func NewInitialization(
	repository model.InitializationRepository,
	options model.InitializationOptions,
) *InitializationService {
	return &InitializationService{repository: repository, options: options}
}

func (service *InitializationService) State(ctx context.Context) (model.InitializationState, error) {
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
			return model.ErrInitializationState
		}
		if service.options.Mode == model.ModeTest {
			_, err := service.bootstrap(ctx, "test", "test", "test", "TEST_DEFAULT")
			return err
		}
		return nil
	}
	if state.State != "COMPLETED" || state.EnabledAdmins == 0 || state.OrphanProfiles != 0 {
		return model.ErrInitializationState
	}
	if service.options.Mode == model.ModeRelease && state.TestDefault {
		return model.ErrTestCredential
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

func (service *InitializationService) ReadSetupCode(ctx context.Context) (string, error) {
	state, err := service.State(ctx)
	if err != nil {
		return "", err
	}
	if state.State != "PENDING" || state.Users != 0 || state.Profiles != 0 {
		return "", model.ErrInitializationDone
	}
	return service.options.Credentials.SetupCode(), nil
}

func (service *InitializationService) Initialize(
	ctx context.Context,
	request model.InitializeRequest,
) (model.Session, error) {
	if service.options.Mode != model.ModeRelease {
		return model.Session{}, model.ErrInitializationDone
	}
	if !service.options.Credentials.MatchesSetupCode(request.SetupCode) {
		return model.Session{}, model.ErrInitializationProof
	}
	username, err := authn.NormalizeUsername(request.Username)
	if err != nil {
		return model.Session{}, fmt.Errorf("normalize initial username: %w", err)
	}
	display, err := authn.NormalizeDisplayName(request.DisplayName)
	if err != nil {
		return model.Session{}, fmt.Errorf("normalize initial display name: %w", err)
	}
	password, err := authn.ValidatePassword(
		request.Password,
		request.PasswordConfirmation,
		username,
		display,
		service.options.Blocklist,
	)
	if err != nil {
		return model.Session{}, fmt.Errorf("validate initial password: %w", err)
	}
	return service.bootstrap(ctx, username, display, password, "RELEASE_SETUP")
}
