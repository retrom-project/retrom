package cleanupjobs

import (
	"context"
	"time"
)

// A removal intent addresses a domain directory or one replaced immutable media
// file. It does not contain a file registry, reference counts or candidates.
type DeletionWriter interface {
	QueuePath(context.Context, string, int64) error
}
type (
	DeletionScope     struct{ Write DeletionWriter }
	DeletionOptions   struct{ Now func() time.Time }
	DeletionScheduler struct{ now func() time.Time }
)

func NewDeletionScheduler(options DeletionOptions) *DeletionScheduler {
	if options.Now == nil {
		options.Now = time.Now
	}
	return &DeletionScheduler{now: options.Now}
}
