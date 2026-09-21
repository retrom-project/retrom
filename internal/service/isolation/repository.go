package isolation

import "context"

type Repository interface {
	Bootstrap(context.Context, TicketQuery) (Bootstrap, error)
	Capability(context.Context, CredentialQuery) (Capability, error)
	Revoke(context.Context, Access, int64) error
	WithWrite(context.Context, func(Tickets) error) error
}

type Tickets interface {
	Bootstrap(context.Context, TicketQuery) (Bootstrap, error)
	Consume(context.Context, TicketQuery, int64) error
	Issue(context.Context, CapabilityWrite) error
}

type RuntimeSession struct {
	Profile, ContentFormat, State string
	Preview                       bool
	HardExpiresAtMS               int64
}

type Bootstrap struct {
	Session     RuntimeSession
	Consumed    bool
	ExpiresAtMS int64
}

type Capability struct {
	Session     RuntimeSession
	Revoked     bool
	ExpiresAtMS int64
}

type TicketQuery struct {
	LaunchID, Origin string
	Digest           *[32]byte
}

type CredentialQuery struct {
	LaunchID, Origin string
	Digest           [32]byte
}

type CapabilityWrite struct {
	Access     Access
	Digest     [32]byte
	IssuedAtMS int64
}
