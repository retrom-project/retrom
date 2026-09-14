package payloadrelease

import (
	"context"
	"fmt"

	application "retrom/internal/model/payloadrelease"
)

func (records gcRecords) Advance(ctx context.Context, change application.GCAdvance) error {
	if err := records.fenceJob(ctx, change.Before); err != nil {
		return err
	}
	args := append([]any{change.ScheduledMS}, gcCandidateArguments(change.Before)...)
	result, err := records.executor.ExecContext(ctx,
		`UPDATE blob_gc_candidates SET scheduled_at_ms=?`+gcCandidateFence, args...)
	if err := gcWrite(result, err); err != nil {
		return fmt.Errorf("advance GC candidate: %w", err)
	}
	work := change.Before.Candidate.Work
	if change.Retry != nil {
		return records.retry(ctx, change)
	}
	if work.State == "RUNNING" {
		return nil
	}
	args = append([]any{change.NowMS, change.NowMS}, workArguments(work)...)
	result, err = records.executor.ExecContext(ctx,
		`UPDATE jobs SET available_at_ms=?,version=version+1,updated_at_ms=?`+workFence, args...)
	if err := gcWrite(result, err); err != nil {
		return fmt.Errorf("advance GC job: %w", err)
	}
	return nil
}

func (records gcRecords) retry(ctx context.Context, change application.GCAdvance) error {
	work, retry := change.Before.Candidate.Work, change.Retry
	err := records.input(ctx, work.ID, retry.ExecutionNo, retry.InputJSON, retry.InputDigest, change.NowMS)
	if err != nil {
		return err
	}
	args := append([]any{retry.ExecutionNo, retry.PayloadJSON, change.NowMS, change.NowMS}, workArguments(work)...)
	result, err := records.executor.ExecContext(ctx, `UPDATE jobs SET state='QUEUED',execution_no=?,payload_json=?,
 attempt_count=0,available_at_ms=?,finished_at_ms=NULL,execution_started_at_ms=NULL,execution_deadline_at_ms=NULL,
 worker_id=NULL,leased_until_ms=NULL,heartbeat_at_ms=NULL,error_code=NULL,error_retryable=NULL,
 version=version+1,updated_at_ms=?`+workFence, args...)
	if err := gcWrite(result, err); err != nil {
		return fmt.Errorf("retry GC job: %w", err)
	}
	return records.event(ctx, work.ID, change.Before.ID, "MANUAL_RETRY", retry.EventJSON, change.NowMS)
}

func (records gcRecords) Audit(ctx context.Context, audit application.GCAudit) error {
	result, err := records.executor.ExecContext(ctx, `INSERT INTO audit_events
 (id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
 before_json,after_json,diff_json,request_id,created_at_ms)
 VALUES(?,'USER',?,NULL,'STORAGE_CLEANUP_REQUESTED','STORAGE','REGISTERED_CAS_PAYLOAD_V1',NULL,?,NULL,NULL,?)`,
		audit.ID, audit.ActorUserID, audit.AfterJSON, audit.NowMS)
	if err := gcWrite(result, err); err != nil {
		return fmt.Errorf("write immediate GC audit: %w", err)
	}
	return nil
}

func (records gcRecords) Cancel(ctx context.Context, change application.GCCancellation) error {
	if err := records.fenceJob(ctx, change.Before); err != nil {
		return err
	}
	result, err := records.executor.ExecContext(ctx, `DELETE FROM blob_gc_candidates`+gcCandidateFence,
		gcCandidateArguments(change.Before)...)
	if err := gcWrite(result, err); err != nil {
		return fmt.Errorf("cancel protected GC candidate: %w", err)
	}
	if !change.Complete {
		return nil
	}
	work := change.Before.Candidate.Work
	args := append([]any{change.NowMS, change.NowMS}, workArguments(work)...)
	result, err = records.executor.ExecContext(ctx, `UPDATE jobs SET state='SUCCEEDED',finished_at_ms=?,
 error_code=NULL,error_retryable=NULL,version=version+1,updated_at_ms=?`+workFence, args...)
	if err := gcWrite(result, err); err != nil {
		return fmt.Errorf("complete protected GC job: %w", err)
	}
	return records.event(ctx, work.ID, change.Before.ID, "SUCCEEDED", change.EventJSON, change.NowMS)
}
