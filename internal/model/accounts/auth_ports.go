package accounts

import (
	"context"

	"retrom/internal/capability/security/authn"
)

type User struct {
	UserID      string `json:"userId"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
}
type Session struct {
	Principal                            authn.Principal
	User                                 User
	CSRFToken                            string
	IdleExpiresAtMS, AbsoluteExpiresAtMS int64
	CookieToken                          string
}
type LoginCredential struct {
	User                            User
	ProfileID, Status, PasswordHash string
	SessionVersion                  int64
}
type SessionSnapshot struct {
	ID, ProfileID, Status                                             string
	User                                                              User
	UserVersion, SessionVersion, LastSeen, IdleExpiry, AbsoluteExpiry int64
	RevokedAt                                                         *int64
}
type SessionRecord struct {
	ID, UserID                                                      string
	Hash                                                            [32]byte
	SessionVersion, CreatedAt, LastSeen, IdleExpiry, AbsoluteExpiry int64
}
type SessionRefresh struct {
	ID                                     string
	ExpectedLastSeen, LastSeen, IdleExpiry int64
}
type AuthReader interface {
	Credential(context.Context, string) (LoginCredential, bool, error)
	Session(context.Context, [32]byte) (SessionSnapshot, bool, error)
}
type AuthWriter interface {
	Login(context.Context, LoginCredential, SessionRecord) error
	Refresh(context.Context, SessionRefresh) error
	Revoke(context.Context, string, int64) error
}
type AuthScope struct {
	Read  AuthReader
	Write AuthWriter
}
type AuthRepository interface {
	Credential(context.Context, string) (LoginCredential, bool, error)
	Session(context.Context, [32]byte) (SessionSnapshot, bool, error)
	CommitWrite(context.Context, func(AuthScope) error) error
}
type PasswordVerifier interface {
	Verify(context.Context, string, string) (bool, error)
}
type SessionMinter func() (SessionMaterial, error)
