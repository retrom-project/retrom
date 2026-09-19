package launch

import (
	"context"

	runtimecontract "retrom/internal/model/runtimecontract"
)

type ConfigSource struct {
	CredentialHash                                                    []byte
	State                                                             string
	Version                                                           int64
	ProviderID, TargetID, BundleDigest, CoreID, CoreName              string
	DetectorProfile, Delivery, Purpose, Title, PlatformName, ReturnTo string
	ContentKind, DependencyJSON, Compatibility                        string
	SaveID, DOSEntry, NetplayID, NetplayRoom, NetplayProfile          *string
	NetplayPlayer                                                     *int64
	BootstrapEnd, HardEnd, InitialDisc                                int64
	IdleEnd                                                           *int64
}

type ConfigFile struct {
	LogicalName, Format, Digest, Role, VirtualPath string
	Size                                           int64
}

type ConfigRestore struct {
	Required, Found bool
	Format, Digest  string
	Size            int64
}

type IsolationGrant struct {
	Origin      string
	TicketHash  []byte
	ExpiresAtMS int64
}

type ConfigAuthority struct {
	Source    ConfigSource
	Restore   ConfigRestore
	Isolation []IsolationGrant
}

type ConfigSnapshot struct {
	Authority ConfigAuthority
	Files     []ConfigFile
}

type ConfigActivationPlan struct {
	Ref            SessionRef
	Version, NowMS int64
}

type ConfigActivation interface {
	Current(context.Context, SessionRef) (ConfigAuthority, bool, error)
	Activate(context.Context, ConfigActivationPlan) error
}

type ConfigAuthorization func(ConfigSource) error

type ConfigRepository interface {
	Load(context.Context, SessionRef, ConfigAuthorization) (ConfigSnapshot, bool, error)
	WithActivation(context.Context, func(ConfigActivation) error) error
}

type ConfigBuilder interface {
	Target(string, string) (runtimecontract.Target, bool)
	BundleSHA256(string, string) (string, bool)
	Build(runtimecontract.LaunchInput) ([]byte, error)
}

type IsolationTicket struct {
	Origin, Ticket string
	Hash           [32]byte
}

type ProjectIdentityReader interface {
	Project(context.Context, string, ConfigAuthorization) (ConfigSnapshot, bool, error)
}
