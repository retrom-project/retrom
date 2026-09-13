package launch

import (
	"context"
	"encoding/json"
	"fmt"

	application "retrom/internal/service/launch"
)

func (records validationWorkerRecords) Claim(ctx context.Context, plan application.ValidationClaimWrite) error {
	before := plan.Before
	result, err := records.executor.ExecContext(ctx, `UPDATE jobs SET state='RUNNING',attempt_count=attempt_count+1,
 execution_started_at_ms=?,execution_deadline_at_ms=?,leased_until_ms=?,heartbeat_at_ms=?,worker_id=?,
 version=version+1,updated_at_ms=? WHERE id=? AND kind='VARIANT_VALIDATE' AND state='QUEUED' AND version=?
 AND execution_no=? AND attempt_count=? AND available_at_ms<=? AND attempt_count<max_attempts`,
		plan.StartedMS, plan.DeadlineMS, plan.LeaseMS, plan.NowMS, plan.WorkerID, plan.NowMS,
		before.ID, before.Version, before.ExecutionNo, before.Attempt, plan.NowMS)
	if err := validationAffected(result, err); err != nil {
		return err
	}
	return records.event(ctx, before, "STARTED", "{}", plan.NowMS)
}

func (records validationWorkerRecords) Renew(
	ctx context.Context,
	claim application.ValidationClaim,
	now, lease int64,
) error {
	work := claim.Job
	result, err := records.executor.ExecContext(ctx, `UPDATE jobs SET leased_until_ms=?,heartbeat_at_ms=?,
 version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING' AND execution_no=? AND attempt_count=?
 AND worker_id=? AND leased_until_ms>? AND execution_deadline_at_ms>?`, lease, now, now, work.ID,
		work.ExecutionNo, work.Attempt, work.WorkerID, now, now)
	return validationAffected(result, err)
}

func (records validationWorkerRecords) Finish(ctx context.Context, plan application.ValidationTerminal) error {
	work := plan.Claim.Job
	var errorCode *string
	var retryable *bool
	var cancellation *int64
	var reason *string
	if plan.State != "SUCCEEDED" {
		errorCode = &plan.Code
		retryable = &plan.Retryable
	}
	if plan.State == "CANCELLED" {
		cancellation = &plan.NowMS
		reason = &plan.Code
	}
	result, err := records.executor.ExecContext(
		ctx,
		`UPDATE jobs SET state=?,error_code=?,error_retryable=?,
 finished_at_ms=?,leased_until_ms=NULL,cancel_requested_at_ms=?,cancel_reason=?,version=version+1,updated_at_ms=?
 WHERE id=? AND kind='VARIANT_VALIDATE' AND state='RUNNING' AND execution_no=? AND attempt_count=? AND worker_id=?`,

		plan.State,
		errorCode,
		retryable,
		plan.NowMS,
		cancellation,
		reason,
		plan.NowMS,
		work.ID,
		work.ExecutionNo,
		work.Attempt,
		work.WorkerID,
	)
	if err := validationAffected(result, err); err != nil {
		return err
	}
	return records.terminalEvent(ctx, work, plan.State, plan.Code, plan.NowMS, plan.Evaluated)
}

func (records validationWorkerRecords) Recover(ctx context.Context, plan application.ValidationRecovery) error {
	before := plan.Before
	if plan.Terminal {
		return records.expire(ctx, plan)
	}
	result, err := records.executor.ExecContext(ctx, `UPDATE jobs SET state='QUEUED',available_at_ms=?,
 leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,version=version+1,updated_at_ms=?
 WHERE id=? AND kind='VARIANT_VALIDATE' AND state='RUNNING' AND version=? AND execution_no=?
 AND leased_until_ms<=?`, plan.NowMS, plan.NowMS, before.ID, before.Version, before.ExecutionNo, plan.NowMS)
	return validationAffected(result, err)
}

func (records validationWorkerRecords) expire(ctx context.Context, plan application.ValidationRecovery) error {
	before := plan.Before
	result, err := records.executor.ExecContext(ctx, `UPDATE jobs SET state='FAILED',
 error_code='LAUNCH_CORE_VALIDATION_UNAVAILABLE',error_retryable=1,finished_at_ms=?,leased_until_ms=NULL,
 version=version+1,updated_at_ms=? WHERE id=? AND kind='VARIANT_VALIDATE' AND state=? AND version=? AND execution_no=?`,
		plan.NowMS, plan.NowMS, before.ID, before.State, before.Version, before.ExecutionNo)
	if err := validationAffected(result, err); err != nil {
		return err
	}
	return records.terminalEvent(ctx, before, "FAILED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE", plan.NowMS, false)
}

func (records validationWorkerRecords) terminalEvent(
	ctx context.Context,
	work application.ValidationWork,
	state, code string,
	now int64,
	evaluated bool,
) error {
	var variantID *string
	if evaluated {
		variantID = &work.ScopeID
	}
	data, err := json.Marshal(struct {
		Code      string  `json:"code"`
		VariantID *string `json:"gameVariantId,omitempty"`
	}{code, variantID})
	if err != nil {
		return fmt.Errorf("encode validation terminal: %w", err)
	}
	return records.event(ctx, work, state, string(data), now)
}

func (records validationWorkerRecords) event(
	ctx context.Context,
	work application.ValidationWork,
	event, data string,
	now int64,
) error {
	_, err := records.executor.ExecContext(ctx, `
 INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
 VALUES(?,?,?,?,?,?)`, work.ID, work.ScopeType, work.ScopeID, event, data, now)
	if err != nil {
		return fmt.Errorf("record validation event: %w", err)
	}
	return nil
}
