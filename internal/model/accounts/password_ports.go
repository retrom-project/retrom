package accounts

import "context"

type PasswordActor struct {
	UserID, SessionID string
	SessionVersion    int64
}
type PasswordState struct {
	Credential     LoginCredential
	SessionCurrent bool
}
type PasswordPlan struct {
	Actor                          PasswordActor
	ExpectedHash, NewHash, AuditID string
	Session                        SessionRecord
	BeforeJSON, AfterJSON          string
	ClearTestDefault               bool
	Now                            int64
}
type PasswordReader interface {
	Current(context.Context, PasswordActor, int64) (PasswordState, bool, error)
}
type PasswordWriter interface {
	Rotate(context.Context, PasswordPlan) error
}

// ChangePasswordCommand captures all pre-computed values for a password change.
type ChangePasswordCommand struct {
	Actor        PasswordActor
	ExpectedHash string
	NewHash      string
	AuditID      string
	Session      SessionRecord
	NowMS        int64
}

type PasswordScope struct {
	Read  PasswordReader
	Write PasswordWriter
}

// PasswordChangeResult carries the data needed by the service to construct
// the refreshed session view after a successful password rotation.
type PasswordChangeResult struct {
	User      User
	ProfileID string
	Version   int64
}

type PasswordRepository interface {
	Current(context.Context, PasswordActor, int64) (PasswordState, bool, error)
	CommitChangePassword(context.Context, ChangePasswordCommand) (PasswordChangeResult, error)
}
type PasswordHasher interface {
	Verify(context.Context, string, string) (bool, error)
	Hash(context.Context, string) (string, error)
}
