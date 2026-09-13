package metadatascrape

import (
	"context"
	"fmt"

	"retrom/internal/service/metadatascrape"
)

func (records mediaRecords) Claim(ctx context.Context, claim metadatascrape.MediaClaim) error {
	if claim.Terminal {
		return mediaChanged(records.executor.ExecContext(ctx, `
UPDATE jobs SET worker_id=?,version=version+1,updated_at_ms=?
 WHERE id=? AND kind='MEDIA_FETCH' AND execution_no=? AND version=? AND
 (state='QUEUED' OR (state IN ('RUNNING','CANCEL_REQUESTED') AND (leased_until_ms<=? OR execution_deadline_at_ms<=?)))`,
			claim.WorkerID, claim.Now, claim.JobID, claim.Execution, claim.Version, claim.Now, claim.Now))
	}
	if err := mediaChanged(records.executor.ExecContext(ctx, `
UPDATE jobs SET state='RUNNING',attempt_count=attempt_count+1,
 execution_started_at_ms=COALESCE(execution_started_at_ms,?),
 execution_deadline_at_ms=COALESCE(execution_deadline_at_ms,?),
 leased_until_ms=?,heartbeat_at_ms=?,worker_id=?,version=version+1,updated_at_ms=?
 WHERE id=? AND kind='MEDIA_FETCH' AND execution_no=? AND version=? AND state='QUEUED'
 AND available_at_ms<=? AND attempt_count<max_attempts
 AND (execution_deadline_at_ms IS NULL OR execution_deadline_at_ms>?)`,
		claim.Now, claim.Deadline, claim.Now+60000, claim.Now, claim.WorkerID, claim.Now, claim.JobID, claim.Execution,
		claim.Version, claim.Now, claim.Now)); err != nil {
		return err
	}
	data := fmt.Sprintf(`{"schemaVersion":1,"executionNo":%d,"attempt":%d}`, claim.Execution, claim.Attempt)
	return records.event(ctx, claim.JobID, "STARTED", data, claim.Now)
}

func (records mediaRecords) Refresh(ctx context.Context, claim metadatascrape.MediaClaim, now int64) error {
	return mediaChanged(records.executor.ExecContext(ctx, `
UPDATE jobs SET leased_until_ms=?,heartbeat_at_ms=?,updated_at_ms=?
 WHERE id=? AND execution_no=? AND attempt_count=? AND worker_id=? AND state='RUNNING'
 AND leased_until_ms>? AND execution_deadline_at_ms>?`,
		now+60000, now, now, claim.JobID, claim.Execution, claim.Attempt,
		claim.WorkerID, now, now))
}

func (records mediaRecords) Requeue(ctx context.Context, claim metadatascrape.MediaClaim, available int64) error {
	if err := mediaChanged(records.executor.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',available_at_ms=?,worker_id=NULL,
 leased_until_ms=NULL,heartbeat_at_ms=NULL,version=version+1,updated_at_ms=?
 WHERE id=? AND kind='MEDIA_FETCH' AND execution_no=? AND version=? AND state='RUNNING'
 AND leased_until_ms<=? AND execution_deadline_at_ms>?`, available, claim.Now, claim.JobID, claim.Execution,
		claim.Version, claim.Now, claim.Now)); err != nil {
		return err
	}
	data := fmt.Sprintf(`{"schemaVersion":1,"executionNo":%d,"attempt":%d,"availableAtMs":%d}`,
		claim.Execution, claim.Attempt, available)
	return records.event(ctx, claim.JobID, "RETRY_SCHEDULED", data, claim.Now)
}

func (records mediaRecords) Finish(ctx context.Context, value metadatascrape.MediaOutcome) error {
	claim := value.Claim
	var code *string
	var retryable *bool
	if value.State == "FAILED" {
		code = &value.Code
		retryable = &value.Retryable
	}
	if err := mediaChanged(records.executor.ExecContext(ctx, `
UPDATE jobs SET state=?,error_code=?,error_retryable=?,
 finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=?,version=version+1,updated_at_ms=?,
 cancel_requested_at_ms=CASE WHEN ?='CANCELLED' THEN COALESCE(cancel_requested_at_ms,?) ELSE NULL END,
 cancel_reason=CASE WHEN ?='CANCELLED' THEN COALESCE(cancel_reason,'MEDIA_OWNER_UNAVAILABLE') ELSE NULL END
 WHERE id=? AND kind='MEDIA_FETCH' AND execution_no=? AND attempt_count=? AND worker_id=?
 AND state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')`, value.State, code, retryable, value.Now, value.Now, value.Now,
		value.State, value.Now, value.State, claim.JobID, claim.Execution, claim.Attempt, claim.WorkerID)); err != nil {
		return err
	}
	data := fmt.Sprintf(`{"schemaVersion":1,"executionNo":%d,"attempt":%d,"code":%q}`,
		claim.Execution, claim.Attempt, value.Code)
	return records.event(ctx, claim.JobID, value.State, data, value.Now)
}
