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
type PasswordScope struct {
	Read  PasswordReader
	Write PasswordWriter
}
type PasswordRepository interface {
	Current(context.Context, PasswordActor, int64) (PasswordState, bool, error)
	CommitWrite(context.Context, func(PasswordScope) error) error
}
type PasswordHasher interface {
	Verify(context.Context, string, string) (bool, error)
	Hash(context.Context, string) (string, error)
}
