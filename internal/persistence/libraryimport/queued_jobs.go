package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/dbexec"
	application "retrom/internal/service/libraryimport"
)

// QueuedJobs reads the scheduling rows used to resume workers after startup.
// Keeping this query here prevents worker lifecycle code from depending on the
// physical jobs table.
type QueuedJobs struct{ executor dbexec.Executor }

var _ application.QueuedJobReader = (*QueuedJobs)(nil)

func NewQueuedJobs(executor dbexec.Executor) *QueuedJobs {
	return &QueuedJobs{executor: executor}
}

func (records *QueuedJobs) Queued(ctx context.Context, kind string) ([]application.QueuedJob, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT id,available_at_ms FROM jobs
WHERE kind=? AND state='QUEUED'
ORDER BY available_at_ms,id
`, kind)
	if err != nil {
		return nil, fmt.Errorf("query queued jobs: %w", err)
	}
	defer func() { cleanup.Error("close queued jobs", rows.Close()) }()
	result, err := collectRows(rows, scanQueuedJob, "scan queued job", "iterate queued jobs")
	if err != nil {
		return nil, err
	}
	return result, nil
}
