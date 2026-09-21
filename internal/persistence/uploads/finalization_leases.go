package uploads

import (
	"context"
	"fmt"

	service "retrom/internal/service/uploads"
)

func (records leaseRecords) Refresh(ctx context.Context, run service.Run, now int64) error {
	return requireChange(records.executor.ExecContext(ctx, `
UPDATE jobs SET heartbeat_at_ms=?,leased_until_ms=?,updated_at_ms=?,version=version+1
WHERE id=? AND execution_no=? AND worker_id=? AND attempt_count=? AND state='RUNNING'
AND execution_deadline_at_ms>? AND leased_until_ms>?`, now, min(now+60000, run.Deadline), now, run.JobID,
		run.ExecutionNo, run.WorkerID, run.Attempt, now, now))
}

func (records leaseRecords) Requeue(ctx context.Context, job service.Job, now, available int64) error {
	if err := requireChange(records.executor.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',worker_id=NULL,leased_until_ms=NULL,heartbeat_at_ms=NULL,
available_at_ms=?,updated_at_ms=?,version=version+1
WHERE id=? AND execution_no=? AND version=? AND state='RUNNING'`,
		available, now, job.ID, job.ExecutionNo, job.Version)); err != nil {
		return err
	}
	data := fmt.Sprintf(`{"schemaVersion":1,"executionNo":%d,"attempt":%d,"availableAtMs":%d}`,
		job.ExecutionNo, job.Attempt, available)
	return jobRecords(records).event(ctx, service.Run{UploadID: job.ScopeID, JobID: job.ID},
		"RETRY_SCHEDULED", []byte(data), now)
}
