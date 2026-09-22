package accounts

import (
	"context"
	"time"

	"retrom/internal/authn"
	"retrom/internal/config"
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
type InitializationScope struct {
	Read  InitializationReader
	Write InitializationWriter
}
type InitializationRepository interface {
	State(context.Context) (InitializationState, error)
	Credentials(context.Context) ([]StoredCredential, error)
	WithWrite(context.Context, func(InitializationScope) error) error
}
type InitializationOptions struct {
	Mode      config.Mode
	Hasher    PasswordHasher
	Blocklist authn.Blocklist
	Mint      SessionMinter
	Now       func() time.Time
}
type InitializeRequest struct{ Username, DisplayName, Password, PasswordConfirmation string }
