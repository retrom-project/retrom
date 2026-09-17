package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/payloadrelease"
)

const workFence = ` WHERE id=? AND kind=? AND scope_type=? AND scope_id=? AND state=?
 AND COALESCE(worker_id,'')=? AND execution_no=? AND attempt_count=? AND max_attempts=? AND version=?
 AND available_at_ms=? AND COALESCE(execution_started_at_ms,-1)=? AND COALESCE(execution_deadline_at_ms,-1)=?
 AND COALESCE(leased_until_ms,-1)=? AND COALESCE(heartbeat_at_ms,-1)=?
 AND ((?=1 AND EXISTS(SELECT 1 FROM job_input_snapshots input WHERE input.job_id=jobs.id
 AND input.execution_no=jobs.execution_no AND input.input_json=? AND input.input_digest=?))
 OR (?=0 AND NOT EXISTS(SELECT 1 FROM job_input_snapshots input
 WHERE input.job_id=jobs.id AND input.execution_no=jobs.execution_no)))`

func workArguments(work application.Work) []any {
	return []any{
		work.ID, work.Kind, work.Scope.Type, work.Scope.ID, work.State, work.WorkerID, work.ExecutionNo,
		work.Attempt, work.MaxAttempts, work.Version, work.AvailableMS, timeFence(work.Started), timeFence(work.Deadline),
		timeFence(work.Lease), timeFence(work.Heartbeat), work.InputFound, work.InputJSON, work.InputDigest, work.InputFound,
	}
}

func timeFence(value application.WorkTime) int64 {
	if !value.Set {
		return -1
	}
	return value.Value
}

func timeArgument(value application.WorkTime) any {
	if !value.Set {
		return nil
	}
	return value.Value
}

func (records workerRecords) Fence(ctx context.Context, work application.Work) error {
	result, err := records.executor.ExecContext(ctx, `UPDATE jobs SET version=version`+workFence, workArguments(work)...)
	return workerWrite(result, err)
}

func (records workerRecords) Change(ctx context.Context, change application.WorkChange) error {
	state, err := workState(change.After.State)
	if err != nil {
		return err
	}
	after := change.After
	var finished, errorCode, retryable, worker any
	if after.State == "SUCCEEDED" || after.State == "FAILED" {
		finished = change.NowMS
	}
	if change.ErrorCode != "" {
		errorCode = change.ErrorCode
		retryable = change.Retryable
	}
	if after.WorkerID != "" {
		worker = after.WorkerID
	}
	values := append(
		make([]any, 0, 32),
		after.Attempt, worker, timeArgument(after.Started), timeArgument(after.Deadline), timeArgument(after.Lease),
		timeArgument(after.Heartbeat), after.Version, after.AvailableMS, finished, errorCode, retryable, change.NowMS,
	)
	values = append(values, workArguments(change.Before)...)
	result, err := records.executor.ExecContext(ctx, `UPDATE jobs SET state=`+state+`,attempt_count=?,worker_id=?,
 execution_started_at_ms=?,execution_deadline_at_ms=?,leased_until_ms=?,heartbeat_at_ms=?,version=?,available_at_ms=?,
 finished_at_ms=?,error_code=?,error_retryable=?,updated_at_ms=?`+workFence, values...)
	if err := workerWrite(result, err); err != nil {
		return fmt.Errorf("write release worker transition: %w", err)
	}
	if change.OwnerFailure != nil {
		if err := records.failOwner(ctx, change); err != nil {
			return err
		}
	}
	return records.evidence(ctx, change)
}

func workState(state string) (string, error) {
	switch state {
	case "RUNNING":
		return "'RUNNING'", nil
	case "QUEUED":
		return "'QUEUED'", nil
	case "SUCCEEDED":
		return "'SUCCEEDED'", nil
	case "FAILED":
		return "'FAILED'", nil
	default:
		return "", application.ErrExecutionLost
	}
}

func workerWrite(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("write release worker record: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count release worker writes: %w", err)
	}
	if count != 1 {
		return application.ErrExecutionLost
	}
	return nil
}

func (records workerRecords) evidence(ctx context.Context, change application.WorkChange) error {
	unit := change.Before
	if change.EventType != "" {
		result, err := records.executor.ExecContext(ctx, `INSERT INTO job_events
 (job_id,scope_type,scope_id,event_type,data_json,created_at_ms) VALUES(?,?,?,?,?,?)`,
			unit.ID, unit.Scope.Type, unit.Scope.ID, change.EventType, change.EventJSON, change.NowMS)
		if err := workerWrite(result, err); err != nil {
			return fmt.Errorf("append release worker event: %w", err)
		}
	}
	if change.AuditID != "" {
		result, err := records.executor.ExecContext(ctx, `INSERT INTO audit_events
 (id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
 before_json,after_json,diff_json,request_id,created_at_ms)
 VALUES(?,'SYSTEM',NULL,'payload-release-worker',?,?,?,NULL,?,NULL,NULL,?)`,
			change.AuditID, change.AuditAction, unit.Scope.Type, unit.Scope.ID, change.AuditJSON, change.NowMS)
		if err := workerWrite(result, err); err != nil {
			return fmt.Errorf("append release worker audit: %w", err)
		}
	}
	return nil
}
