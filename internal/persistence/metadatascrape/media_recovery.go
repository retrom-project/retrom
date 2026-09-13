package metadatascrape

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
)

func (repository *MediaRepository) Recoverable(ctx context.Context, now int64) ([]string, error) {
	rows, err := repository.database.QueryContext(ctx, `SELECT j.id FROM jobs j
 LEFT JOIN scrape_candidate_assets a ON a.media_fetch_job_id=j.id
 LEFT JOIN scrape_candidates c ON c.id=a.scrape_candidate_id
 LEFT JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
 WHERE j.kind='MEDIA_FETCH' AND (
 (j.state='QUEUED' AND (r.state<>'RUNNING' OR r.id IS NULL) AND
 (j.available_at_ms<=? OR j.execution_deadline_at_ms<=?) AND
 (a.media_fetch_order IS NULL OR NOT EXISTS(SELECT 1 FROM scrape_candidate_assets earlier
 JOIN scrape_candidates ec ON ec.id=earlier.scrape_candidate_id JOIN jobs ej ON ej.id=earlier.media_fetch_job_id
 WHERE ec.scrape_run_id=r.id AND earlier.media_fetch_order<a.media_fetch_order
 AND ej.state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')))) OR
 (j.state IN ('RUNNING','CANCEL_REQUESTED') AND (j.leased_until_ms<=? OR j.execution_deadline_at_ms<=?)) OR
 (j.state='CANCELLED' AND a.status NOT IN ('CANCELLED','READY')))
 ORDER BY j.available_at_ms,j.created_at_ms,j.id LIMIT 64`, now, now, now, now)
	if err != nil {
		return nil, fmt.Errorf("query media recovery queue: %w", err)
	}
	defer func() { cleanup.Error("close media recovery", rows.Close()) }()
	return readRecoveryIDs(rows)
}
