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
type LinkIssueRepository interface {
	CommitIssue(context.Context, LinkIssueCommand) (LinkIssueResult, error)
}

// LinkIssueCommand carries the values for an atomic link issuance.
type LinkIssueCommand struct {
	Operation AccountOperation
	Plan      LinkIssuePlan
	Audit     AccountAudit
	Receipt   AccountReceipt
}

// LinkIssueResult carries the outcome of a link issuance.
type LinkIssueResult struct {
	Link     AccountLink
	Replayed bool
}
