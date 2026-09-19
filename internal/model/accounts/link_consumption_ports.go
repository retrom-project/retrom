package accounts

import (
	"context"
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
type LinkConsumptionReader interface {
	Current(context.Context, string) (LinkRecord, bool, error)
	Target(context.Context, string) (LinkTarget, bool, error)
	UsernameExists(context.Context, string) (bool, error)
}
type LinkConsumptionWriter interface {
	Accept(context.Context, InvitationAcceptance) error
	Reset(context.Context, ResetConsumption) error
	Audit(context.Context, AccountAudit) error
}
type LinkConsumptionScope struct {
	Read  LinkConsumptionReader
	Write LinkConsumptionWriter
}
type LinkConsumptionRepository interface {
	ResetState(context.Context, string) (ResetState, bool, error)
	WithConsumptionWrite(context.Context, func(LinkConsumptionScope) error) error
}
