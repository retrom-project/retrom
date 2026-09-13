package libraryimport

import "context"

// QueuedJob is the minimal scheduling state needed to resume a worker after
// process startup. The application layer does not depend on the jobs table
// shape; persistence maps its rows into this value.
type QueuedJob struct {
	ID            string
	AvailableAtMS int64
}

// QueuedJobReader loads queued jobs for a worker kind. Implementations belong
// to the persistence layer so worker orchestration can remain database agnostic.
type QueuedJobReader interface {
	Queued(context.Context, string) ([]QueuedJob, error)
}
