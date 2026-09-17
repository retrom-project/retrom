package tagging

import "context"

// CrossDomainWriter provides atomic tagging operations for callers in other
// domains that hold their own transaction. The implementation lives in the
// repository layer; model defines only the contract.
type CrossDomainWriter interface {
	ValidateActiveReferences(ctx context.Context, tagIDs []string) ([]Reference, error)
	ReplaceOwnerReferences(
		ctx context.Context, owner Owner, tagIDs []string,
		actorUserID string, now int64,
	) (before []Reference, after []Reference, err error)
	AssignReferences(
		ctx context.Context, owner Owner, refs []Reference,
		actorUserID string, now int64,
	) error
	ReadOwnerReferences(ctx context.Context, owner Owner) ([]Reference, error)
	CopyOwnerReferences(
		ctx context.Context, from, to Owner,
		actorUserID string, now int64,
	) ([]Reference, error)
}
