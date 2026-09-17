package isolation

import (
	"context"
	"errors"
)

var ErrCredential = errors.New("RPG_ISOLATED_RUNTIME_CREDENTIAL_INVALID")

// ConsumeAndIssueCommand captures all inputs for the atomic
// ticket-consumption + capability-issuance transaction.
type ConsumeAndIssueCommand struct {
	Query    TicketQuery
	LaunchID string
	Origin   string
	Digest   [32]byte
	NowMS    int64
}

// ConsumeAndIssueResult carries the access derived from the bootstrap.
type ConsumeAndIssueResult struct {
	Access Access
}

type Repository interface {
	Bootstrap(context.Context, TicketQuery) (Bootstrap, error)
	Capability(context.Context, CredentialQuery) (Capability, error)
	Revoke(context.Context, Access, int64) error
	ConsumeAndIssue(context.Context, ConsumeAndIssueCommand) (ConsumeAndIssueResult, error)
}

type Access struct {
	LaunchID      string
	Origin        string
	Profile       string
	ContentFormat string
	Preview       bool
	Expires       int64
}

type RuntimeSession struct {
	Profile, ContentFormat, State string
	Preview                       bool
	HardExpiresAtMS               int64
}

// ActiveSession returns true when the session is active for isolated runtime use.
func ActiveSession(session RuntimeSession, nowMS int64) bool {
	return session.State == "ACTIVE" && session.HardExpiresAtMS > nowMS &&
		(session.ContentFormat == "RPG_MAKER_PROJECT" || session.ContentFormat == "TYRANOSCRIPT_PROJECT")
}

// SessionAccess builds an Access value from a bootstrap session.
func SessionAccess(session RuntimeSession, launchID, origin string, expires int64) Access {
	return Access{
		LaunchID: launchID, Origin: origin,
		Profile:       session.Profile,
		ContentFormat: session.ContentFormat,
		Preview:       session.Preview,
		Expires:       expires,
	}
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
