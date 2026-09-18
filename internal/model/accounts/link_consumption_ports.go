package accounts

import (
	"context"
	"time"

	"retrom/internal/capability/security/authn"
)

type (
	AcceptInvitationRequest      struct{ Token, Username, DisplayName, Password, PasswordConfirmation string }
	CompletePasswordResetRequest struct{ Token, Password, PasswordConfirmation string }
	PasswordResetResult          struct {
		Session *Session
		Status  string
	}
)

type ResetState struct {
	Link   LinkRecord
	Target LinkTarget
}
type InvitationAcceptance struct {
	LinkID                  string
	LinkVersion             int64
	User                    User
	ProfileID, PasswordHash string
	Session                 SessionRecord
	Now                     int64
}
type ResetConsumption struct {
	LinkID           string
	LinkVersion      int64
	Target           LinkTarget
	PasswordHash     string
	Session          *SessionRecord
	ClearTestDefault bool
	Now              int64
}
type InvitationAcceptCommand struct {
	Plan  InvitationAcceptance
	Audit AccountAudit
}
type PasswordResetCommand struct {
	Plan  ResetConsumption
	Audit AccountAudit
}
type LinkConsumptionRepository interface {
	ResetState(context.Context, string) (ResetState, bool, error)
	LoadInvitationLink(ctx context.Context, id string) (LinkRecord, bool, error)
	CommitInvitationAcceptance(ctx context.Context, cmd InvitationAcceptCommand) error
	CommitPasswordReset(ctx context.Context, cmd PasswordResetCommand) error
}
type LinkConsumptionOptions struct {
	Tokens    LinkTokenReader
	Hasher    PasswordHasher
	Blocklist authn.Blocklist
	Mint      SessionMinter
	Now       func() time.Time
}
