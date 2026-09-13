package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/persistence/dbexec"
	application "retrom/internal/service/libraryimport"
)

// ReviewArcadeParentJobs reads the queue used to resume arcade parent
// attachment validation after process startup. Keeping this query in the
// persistence layer leaves the worker lifecycle in the application package.
type ReviewArcadeParentJobs struct{ executor dbexec.Executor }

var _ application.ArcadeParentJobQueue = (*ReviewArcadeParentJobs)(nil)

func NewReviewArcadeParentJobs(executor dbexec.Executor) *ReviewArcadeParentJobs {
	return &ReviewArcadeParentJobs{executor: executor}
}

func (records *ReviewArcadeParentJobs) Queued(ctx context.Context) ([]string, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT id FROM jobs
WHERE kind='REVIEW_ARCADE_PARENT_VALIDATE' AND state='QUEUED'
ORDER BY available_at_ms,id
`)
	if err != nil {
		return nil, fmt.Errorf("query arcade parent jobs: %w", err)
	}
	defer func() { cleanup.Error("close arcade parent jobs", rows.Close()) }()
	jobIDs := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan arcade parent job: %w", err)
		}
		jobIDs = append(jobIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate arcade parent jobs: %w", err)
	}
	return jobIDs, nil
}
