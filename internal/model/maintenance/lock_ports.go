package maintenance

import "context"

// DataRootLease is an owned operating-system resource, never a transaction value.
type DataRootLease interface {
	Close() error
}

// DataRootLocker acquires the same nonblocking lease used by the server and CLI.
type DataRootLocker interface {
	Acquire(ctx context.Context, dataRoot string) (DataRootLease, error)
}
