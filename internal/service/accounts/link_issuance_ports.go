package accounts

import (
	"context"

	"github.com/google/uuid"
)

type LinkTarget struct {
	User                    User
	ProfileID, Status       string
	Version, SessionVersion int64
}
type LinkIssuePlan struct {
	Link           AccountLink
	Target         *LinkTarget
	RevokePrevious bool
}
type LinkIssuer interface {
	AccountLinkToken(string, uuid.UUID) string
}
type LinkIssueReader interface {
	Target(context.Context, string) (LinkTarget, bool, error)
	Replay(context.Context, AccountOperation) (AccountReplay, error)
}
type LinkIssueWriter interface {
	Issue(context.Context, LinkIssuePlan) error
	Audit(context.Context, AccountAudit) error
	Remember(context.Context, AccountReceipt) error
}
type LinkIssueScope struct {
	Read  LinkIssueReader
	Write LinkIssueWriter
}
type LinkIssueRepository interface {
	WithIssueWrite(context.Context, func(LinkIssueScope) error) error
}
