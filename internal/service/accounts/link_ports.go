package accounts

import (
	"context"

	"github.com/google/uuid"
)

type LinkCreator struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
}
type AccountLink struct {
	AccountLinkID   string       `json:"accountLinkId"`
	Kind            string       `json:"kind"`
	Role            *string      `json:"role"`
	TargetUserID    *string      `json:"targetUserId"`
	CreatedBy       *LinkCreator `json:"createdBy"`
	State           string       `json:"state"`
	Version         int64        `json:"version"`
	CreatedAtMS     int64        `json:"createdAtMs"`
	ExpiresAtMS     int64        `json:"expiresAtMs"`
	ConsumedAtMS    *int64       `json:"consumedAtMs"`
	RevokedAtMS     *int64       `json:"revokedAtMs"`
	TargetVersion   int64        `json:"targetUserVersion,omitempty"`
	CapabilityToken string       `json:"-"`
}
type LinkInspection struct {
	Kind        string  `json:"kind"`
	Role        *string `json:"role"`
	Username    *string `json:"username"`
	ExpiresAtMS int64   `json:"expiresAtMs"`
}
type LinkRecord struct {
	Link           AccountLink
	TargetUsername *string
}
type LinkListFilter struct {
	Kind, TargetUserID, State string
	AfterAtMS                 int64
	AfterID                   string
	Limit                     int
}
type LinkQuery struct {
	Filter LinkListFilter
	Now    int64
}
type LinkRevocation struct {
	LinkID, ActorID string
	Version, Now    int64
}
type LinkTokenReader interface {
	ParseAccountLinkToken(string, string) (uuid.UUID, bool)
}
type LinkReader interface {
	Current(context.Context, string) (LinkRecord, bool, error)
	Replay(context.Context, AccountOperation) (AccountReplay, error)
}
type LinkWriter interface {
	Revoke(context.Context, LinkRevocation) error
	Audit(context.Context, AccountAudit) error
	Remember(context.Context, AccountReceipt) error
}
type LinkScope struct {
	Read  LinkReader
	Write LinkWriter
}
type LinkRepository interface {
	Current(context.Context, string) (LinkRecord, bool, error)
	List(context.Context, LinkQuery) ([]LinkRecord, error)
	WithWrite(context.Context, func(LinkScope) error) error
}
