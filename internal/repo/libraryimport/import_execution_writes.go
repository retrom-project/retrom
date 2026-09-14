package libraryimport

import (
	"context"
	"math"

	application "retrom/internal/model/libraryimport"
)

func (records importExecutionRecords) Transition(
	ctx context.Context,
	change application.ImportWorkerTransition,
) error {
	before := change.Before.Creation
	if before.JobVersion < 1 || before.JobVersion == math.MaxInt64 || before.ParentVersion < 1 ||
		before.ParentVersion == math.MaxInt64 {
		return application.ErrVersionConflict
	}
	if !change.ParentOnly {
		if err := records.writeJob(ctx, change); err != nil {
			return err
		}
	}
	if change.Parent != nil {
		if err := records.writeParent(ctx, change); err != nil {
			return err
		}
	}
	if change.Event != nil {
		event := change.Event
		result, err := records.executor.ExecContext(
			ctx,
			`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_GROUP',?,?,?,?)`,
			event.JobID,
			event.ScopeID,
			event.Kind,
			event.DataJSON,
			event.NowMS,
		)
		if err := creationMutation(result, err, "record import execution event", 1); err != nil {
			return err
		}
	}
	return nil
}

func (records importExecutionRecords) writeJob(
	ctx context.Context,
	change application.ImportWorkerTransition,
) error {
	before := change.Before.Creation
	old := before.Execution
	job := change.Job
	execution := job.Execution
	result, err := records.executor.ExecContext(
		ctx,
		`
UPDATE jobs SET state=?,worker_id=?,attempt_count=?,execution_started_at_ms=?,execution_deadline_at_ms=?,
 available_at_ms=?,leased_until_ms=?,heartbeat_at_ms=?,finished_at_ms=?,error_code=?,error_retryable=?,
 cancel_requested_at_ms=?,cancel_reason=?,version=version+1,updated_at_ms=?
WHERE id=? AND kind='IMPORT_GROUP' AND scope_type='IMPORT_GROUP' AND scope_id=? AND version=? AND state=?
 AND COALESCE(worker_id,'')=? AND execution_no=? AND attempt_count=?
 AND COALESCE(execution_started_at_ms,0)=? AND COALESCE(execution_deadline_at_ms,0)=?
 AND COALESCE(leased_until_ms,0)=? AND (?=0 OR leased_until_ms>?)`,
		job.State,
		creationNullable(execution.WorkerID),
		execution.Attempt,
		job.StartedAtMS,
		job.DeadlineAtMS,
		job.AvailableAtMS,
		job.LeaseUntilMS,
		job.HeartbeatAtMS,
		job.FinishedAtMS,
		job.ErrorCode,
		job.Retryable,
		job.CancelRequestedAtMS,
		job.CancelReason,
		change.AtMS,
		old.JobID,
		old.ImportID,
		before.JobVersion,
		before.JobState,
		old.WorkerID,
		old.ExecutionNo,
		old.Attempt,
		old.StartedAtMS,
		old.DeadlineMS,
		before.LeaseUntilMS,
		change.RequireLiveLease,
		change.AtMS,
	)
	return creationMutation(result, err, "write import execution job", 1)
}

func (records importExecutionRecords) writeParent(
	ctx context.Context,
	change application.ImportWorkerTransition,
) error {
	before := change.Before.Creation
	parent := change.Parent
	version := before.JobVersion
	if !change.ParentOnly {
		version++
	}
	result, err := records.executor.ExecContext(
		ctx,
		`
UPDATE import_jobs SET state=?,completed_at_ms=?,cancel_requested_at_ms=?,cancel_reason=?,
 last_error_code=?,version=version+1,updated_at_ms=?
WHERE id=? AND version=? AND state=? AND EXISTS(SELECT 1 FROM jobs WHERE id=? AND
 scope_id=import_jobs.id AND version=?)`,
		parent.State,
		parent.CompletedAtMS,
		parent.CancelRequestedAtMS,
		parent.CancelReason,
		parent.ErrorCode,
		change.AtMS,
		before.Execution.ImportID,
		before.ParentVersion,
		before.ImportState,
		before.Execution.JobID,
		version,
	)
	return creationMutation(result, err, "write import execution parent", 1)
}
