package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	application "retrom/internal/model/libraryimport"
)

func (records importExecutionRecords) Current(
	ctx context.Context,
	id string,
) (application.ImportWorkerSnapshot, bool, error) {
	var result application.ImportWorkerSnapshot
	c := &result.Creation
	e := &c.Execution
	counts := &result.Counts
	err := records.executor.QueryRowContext(
		ctx,
		`
SELECT job.id,job.scope_id,COALESCE(job.worker_id,''),COALESCE(request.actor_user_id,''),
job.execution_no,job.attempt_count,job.execution_started_at_ms,
 job.execution_deadline_at_ms,
job.state,parent.state,job.version,parent.version,job.max_attempts,
COALESCE(request.request_json,''),COALESCE(request.request_digest,''),
 COALESCE(request.target_snapshot_json,''),
COALESCE(request.target_snapshot_digest,''),parent.upload_session_id,COALESCE(request.upload_version,
 0),COALESCE(request.upload_manifest_digest,''),
request.import_job_id IS NOT NULL,job.cancellable,job.available_at_ms,job.leased_until_ms,
 job.heartbeat_at_ms,job.finished_at_ms,
job.error_code,job.error_retryable,job.cancel_requested_at_ms,job.cancel_reason,
parent.completed_at_ms,parent.cancel_requested_at_ms,parent.cancel_reason,parent.last_error_code,
parent.queued_item_count,parent.running_item_count,parent.review_pending_item_count,
 parent.failed_item_count,
parent.cancelled_item_count,parent.rejected_file_count,parent.resolved_rejected_file_count,
(SELECT count(*) FROM import_items WHERE import_job_id=parent.id),
(SELECT count(*) FROM import_job_files WHERE import_job_id=parent.id AND disposition!='PENDING')
FROM jobs job JOIN import_jobs parent ON parent.id=job.scope_id
LEFT JOIN import_group_requests request ON request.import_job_id=parent.id
WHERE job.id=? AND job.kind='IMPORT_GROUP' AND job.scope_type='IMPORT_GROUP'`,
		id,
	).Scan(
		&e.JobID,
		&e.ImportID,
		&e.WorkerID,
		&e.ActorUserID,
		&e.ExecutionNo,
		&e.Attempt,
		&result.StartedAtMS,
		&result.DeadlineAtMS,
		&c.JobState,
		&c.ImportState,
		&c.JobVersion,
		&c.ParentVersion,
		&c.MaxAttempts,
		&c.RequestJSON,
		&c.RequestDigest,
		&c.TargetJSON,
		&c.TargetDigest,
		&c.UploadID,
		&c.UploadVersion,
		&c.UploadDigest,
		&result.HasRequest,
		&result.Cancellable,
		&result.AvailableAtMS,
		&result.LeaseUntilMS,
		&result.HeartbeatAtMS,
		&result.FinishedAtMS,
		&result.ErrorCode,
		&result.Retryable,
		&result.CancelRequestedAtMS,
		&result.CancelReason,
		&result.ParentCompletedAtMS,
		&result.ParentCancelRequestedAtMS,
		&result.ParentCancelReason,
		&result.ParentErrorCode,
		&counts.Queued,
		&counts.Running,
		&counts.ReviewPending,
		&counts.Failed,
		&counts.Cancelled,
		&counts.Rejected,
		&counts.ResolvedRejected,
		&result.ItemCount,
		&result.ResolvedFiles,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ImportWorkerSnapshot{}, false, nil
	}
	if err != nil {
		return application.ImportWorkerSnapshot{}, false, fmt.Errorf("query import execution snapshot: %w", err)
	}
	if result.StartedAtMS != nil {
		e.StartedAtMS = *result.StartedAtMS
	}
	if result.DeadlineAtMS != nil {
		e.DeadlineMS = *result.DeadlineAtMS
	}
	if result.LeaseUntilMS != nil {
		c.LeaseUntilMS = *result.LeaseUntilMS
	}
	return result, true, nil
}
