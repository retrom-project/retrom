package blob

import "context"

// DigestCoordinator serializes publication, protective reference commits and
// physical deletion for canonical lowercase SHA-256 digests.
type DigestCoordinator interface {
	Acquire(context.Context, []string) (DigestLease, error)
}

// DigestLease is an owned resource. It must not enter a command or snapshot,
// or cross into a repository transaction.
type DigestLease interface {
	Release() error
}
