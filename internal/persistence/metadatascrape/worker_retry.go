package metadatascrape

import (
	"context"
	"fmt"

	"retrom/internal/service/metadatascrape"
)

func (records workerRecords) Requeue(
	ctx context.Context, claim metadatascrape.WorkerClaim, available int64,
) (bool, error) {
	changed, err := workerChanged(records.transaction.ExecContext(ctx, `UPDATE jobs SET state='QUEUED',
 available_at_ms=?,leased_until_ms=NULL,worker_id=NULL,heartbeat_at_ms=NULL,version=version+1,updated_at_ms=?
 WHERE id=? AND execution_no=? AND version=? AND kind='METADATA_SCRAPE' AND state='RUNNING'
 AND leased_until_ms<=? AND execution_deadline_at_ms>? AND attempt_count<max_attempts
 AND EXISTS(SELECT 1 FROM metadata_scrape_runs r WHERE r.id=? AND r.job_id=jobs.id AND r.state='RUNNING')`,
		available, claim.Now, claim.JobID, claim.ExecutionNo, claim.Version, claim.Now, claim.Now, claim.RunID))
	if err != nil || !changed {
		return changed, err
	}
	data := fmt.Sprintf(`{"availableAtMs":%d,"attempt":%d}`, available, claim.AttemptCount+1)
	if err := records.event(ctx, claim.JobID, "RETRY_SCHEDULED", data, claim.Now); err != nil {
		return false, err
	}
	return true, nil
}
