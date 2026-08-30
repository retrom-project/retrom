package libraryimport

import (
	"context"

	"retrom/internal/cleanup"
)

type queuedJobRun struct {
	id          string
	availableAt int64
}

func (service *Service) queuedJobRuns(ctx context.Context, kind string) []queuedJobRun {
	rows, err := service.database.QueryContext(ctx, `
SELECT id,available_at_ms FROM jobs
WHERE kind=? AND state='QUEUED'
ORDER BY available_at_ms,id
`, kind)
	if err != nil {
		return nil
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	queued := make([]queuedJobRun, 0)
	for rows.Next() {
		var job queuedJobRun
		if rows.Scan(&job.id, &job.availableAt) == nil {
			queued = append(queued, job)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return queued
}
