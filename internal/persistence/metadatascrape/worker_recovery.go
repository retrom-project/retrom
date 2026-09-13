package metadatascrape

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
)

func (repository *WorkerRepository) Recoverable(ctx context.Context, now int64) ([]string, error) {
	rows, err := repository.database.QueryContext(ctx, `SELECT r.id FROM metadata_scrape_runs r
 JOIN jobs j ON j.id=r.job_id WHERE r.state='RUNNING' AND r.provider='HASHEOUS' AND j.kind='METADATA_SCRAPE'
 AND ((j.state='QUEUED' AND
 (j.available_at_ms<=? OR j.execution_deadline_at_ms<=? OR j.attempt_count>=j.max_attempts)) OR
 (j.state IN ('RUNNING','CANCEL_REQUESTED') AND (j.leased_until_ms<=? OR j.execution_deadline_at_ms<=?))
 OR j.state='CANCELLED')
 ORDER BY j.available_at_ms,j.id LIMIT 64`, now, now, now, now)
	if err != nil {
		return nil, fmt.Errorf("query recoverable metadata executions: %w", err)
	}
	defer func() { cleanup.Error("close recoverable metadata executions", rows.Close()) }()
	return readRecoveryIDs(rows)
}
