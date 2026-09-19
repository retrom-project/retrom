package accounts

import (
	"context"
)

// Mode represents the application operating mode passed to model from the
// service layer.  Defining it here avoids a model → bootstrap dependency.
type Mode string

const (
	ModeRelease Mode = "release"
	ModeTest    Mode = "test"
)

type InitializationState struct {
	State                                          string
	TestDefault                                    bool
	Users, Profiles, EnabledAdmins, OrphanProfiles int
}
type StoredCredential struct {
	Scheme, Hash string
	Missing      bool
}
type BootstrapPlan struct {
	UserID, ProfileID, Username, DisplayName, PasswordHash, Kind, ActorLabel, AuditID string
	TestDefault                                                                       bool
	Session                                                                           SessionRecord
	Now                                                                               int64
}
type InitializationReader interface {
	State(context.Context) (InitializationState, error)
}
type InitializationWriter interface {
	Bootstrap(context.Context, BootstrapPlan) error
}

// BootstrapCommand captures all inputs for initializing the account store.
type BootstrapCommand struct {
	Plan BootstrapPlan
}

type InitializationScope struct {
	Read  InitializationReader
	Write InitializationWriter
}
type InitializationRepository interface {
	State(context.Context) (InitializationState, error)
	Credentials(context.Context) ([]StoredCredential, error)
	CommitBootstrap(context.Context, BootstrapCommand) error
}
type SetupCredentials interface {
	SetupCode() string
	MatchesSetupCode(string) bool
}
type InitializeRequest struct{ SetupCode, Username, DisplayName, Password, PasswordConfirmation string }
