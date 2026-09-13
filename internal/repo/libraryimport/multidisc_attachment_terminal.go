package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
	application "retrom/internal/service/libraryimport"
)

var _ application.MultiDiscAttachmentTerminalRepository = (*MultiDiscAttachmentFinalization)(nil)

func (repository *MultiDiscAttachmentFinalization) Reject(
	ctx context.Context, write application.MultiDiscAttachmentRejectWrite,
) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin multi-disc rejection: %w", err)
	}
	defer dbexec.Rollback(transaction)
	result, err := recordstore.UpdateReviewMultidiscAttachments(ctx, transaction, recordstore.Update{
		Set: `state='REJECTED',error_code=?,diagnostics_json=?,finished_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND state='RUNNING'`, Args: []any{write.Target.AttachmentID},
		},
		Values: []any{write.Code, write.DiagnosticsJSON, write.NowMS, write.NowMS},
	})
	if err := requireMultiDiscChange(result, err, "reject attachment"); err != nil {
		return err
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code=?,error_retryable=0,finished_at_ms=?,
leased_until_ms=NULL,heartbeat_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, write.Code, write.NowMS, write.NowMS, write.Target.JobID, write.Target.WorkerID)
	if err := requireMultiDiscChange(result, err, "fail rejected attachment job"); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms) VALUES
(?,'IMPORT_ITEM',?,'DISC_SET_REJECTED',?,?),
(?,'IMPORT_ITEM',?,'FAILED',?,?)
`, write.Target.JobID, write.Target.ItemID, write.DiagnosticsJSON, write.NowMS,
		write.Target.JobID, write.Target.ItemID, write.DiagnosticsJSON, write.NowMS); err != nil {
		return fmt.Errorf("record rejected multi-disc job events: %w", err)
	}
	actorUserID := nullableStringValue(write.Actor.UserID)
	actorLabel := nullableStringValue(write.Actor.Label)
	if _, err := recordstore.CreateReviewEvents(ctx, transaction, `
INSERT INTO review_events(id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,
before_json,after_json,diff_json,config_evidence_json,dat_evidence_json,provider_evidence_json,created_at_ms)
VALUES(?,?,'DISC_ATTACHMENT_REJECTED',?,?,?,?,?,?,?,?,?,?)
`, write.EventID, write.Target.ItemID, write.Actor.Kind, actorUserID, actorLabel,
		`{"schemaVersion":2}`, write.EvidenceJSON, write.EvidenceJSON, `{"schemaVersion":2}`,
		`{"schemaVersion":2}`, `{"schemaVersion":2}`, write.NowMS); err != nil {
		return fmt.Errorf("record rejected multi-disc review event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit rejected multi-disc attachment: %w", err)
	}
	return nil
}

func (repository *MultiDiscAttachmentFinalization) TryRetry(
	ctx context.Context, write application.MultiDiscAttachmentRetryWrite,
) (application.MultiDiscAttachmentRetryResult, error) {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return application.MultiDiscAttachmentRetryResult{}, fmt.Errorf("begin multi-disc retry: %w", err)
	}
	defer dbexec.Rollback(transaction)
	var attemptCount, maxAttempts, deadline int64
	if err := transaction.QueryRowContext(ctx, `
SELECT attempt_count,max_attempts,execution_deadline_at_ms
FROM jobs WHERE id=? AND state='RUNNING' AND worker_id=?
`, write.Target.JobID, write.Target.WorkerID).Scan(&attemptCount, &maxAttempts, &deadline); err != nil {
		return application.MultiDiscAttachmentRetryResult{}, fmt.Errorf("read multi-disc retry job: %w", err)
	}
	delay := multiDiscAttachmentRetryDelay(attemptCount)
	availableAt := write.NowMS + delay.Milliseconds()
	if attemptCount >= maxAttempts || availableAt >= deadline {
		return application.MultiDiscAttachmentRetryResult{}, nil
	}
	result, err := recordstore.UpdateReviewMultidiscAttachments(ctx, transaction, recordstore.Update{
		Set:    `state='FAILED_RETRYABLE',error_code=?,diagnostics_json=?,finished_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope:  recordstore.Scope{Where: `id=? AND state='RUNNING'`, Args: []any{write.Target.AttachmentID}},
		Values: []any{write.Code, write.DiagnosticsJSON, write.NowMS, write.NowMS},
	})
	if err := requireMultiDiscChange(result, err, "schedule retry attachment"); err != nil {
		return application.MultiDiscAttachmentRetryResult{}, err
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',available_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
worker_id=NULL,error_code=NULL,error_retryable=NULL,finished_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, availableAt, write.NowMS, write.Target.JobID, write.Target.WorkerID)
	if err := requireMultiDiscChange(result, err, "queue multi-disc retry job"); err != nil {
		return application.MultiDiscAttachmentRetryResult{}, err
	}
	eventJSON, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "attempt": attemptCount, "retryAtMs": availableAt,
		"durationMs": multiDiscAttachmentDurationMS(write.Target.ExecutionStartedAtMS, write.NowMS),
		"errorCode":  write.Code,
	})
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'RETRY_SCHEDULED',?,?)
`, write.Target.JobID, write.Target.ItemID, string(eventJSON), write.NowMS); err != nil {
		return application.MultiDiscAttachmentRetryResult{}, fmt.Errorf("record multi-disc retry event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return application.MultiDiscAttachmentRetryResult{}, fmt.Errorf("commit multi-disc retry: %w", err)
	}
	return application.MultiDiscAttachmentRetryResult{Scheduled: true, Delay: delay}, nil
}

func (repository *MultiDiscAttachmentFinalization) FailRetryable(
	ctx context.Context, write application.MultiDiscAttachmentRetryWrite,
) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin multi-disc retry failure: %w", err)
	}
	defer dbexec.Rollback(transaction)
	result, err := recordstore.UpdateReviewMultidiscAttachments(ctx, transaction, recordstore.Update{
		Set:    `state='FAILED_RETRYABLE',error_code=?,diagnostics_json=?,finished_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope:  recordstore.Scope{Where: `id=? AND state='RUNNING'`, Args: []any{write.Target.AttachmentID}},
		Values: []any{write.Code, write.DiagnosticsJSON, write.NowMS, write.NowMS},
	})
	if err := requireMultiDiscChange(result, err, "fail retryable attachment"); err != nil {
		return err
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code=?,error_retryable=1,finished_at_ms=?,
leased_until_ms=NULL,heartbeat_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, write.Code, write.NowMS, write.NowMS, write.Target.JobID, write.Target.WorkerID)
	if err := requireMultiDiscChange(result, err, "fail retryable attachment job"); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'FAILED',?,?)
`, write.Target.JobID, write.Target.ItemID, write.DiagnosticsJSON, write.NowMS); err != nil {
		return fmt.Errorf("record failed multi-disc retry event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit failed multi-disc retry: %w", err)
	}
	return nil
}

func (repository *MultiDiscAttachmentFinalization) SyncCancellation(
	ctx context.Context, jobID string, nowMS int64,
) error {
	_, err := recordstore.UpdateReviewMultidiscAttachments(ctx, repository.database, recordstore.Update{
		Set: `state='CANCELLED',error_code='CANCELLED',
diagnostics_json='{"errorCode":"CANCELLED","schemaVersion":1}',finished_at_ms=?,
version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{Where: `job_id=? AND state IN ('QUEUED','RUNNING','FAILED_RETRYABLE')
AND EXISTS(SELECT 1 FROM jobs WHERE id=? AND state='CANCELLED')`, Args: []any{jobID, jobID}},
		Values: []any{nowMS, nowMS},
	})
	if err != nil {
		return fmt.Errorf("sync cancelled multi-disc attachment: %w", err)
	}
	return nil
}

func (repository *MultiDiscAttachmentFinalization) FinishCancellation(
	ctx context.Context, write application.MultiDiscAttachmentCancellationWrite,
) (bool, error) {
	var state string
	if err := repository.database.QueryRowContext(ctx,
		`SELECT state FROM jobs WHERE id=? AND worker_id=?`, write.Target.JobID, write.Target.WorkerID,
	).Scan(&state); err != nil {
		return false, fmt.Errorf("read multi-disc cancellation state: %w", err)
	}
	if state != "CANCEL_REQUESTED" {
		return false, nil
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin multi-disc cancellation: %w", err)
	}
	defer dbexec.Rollback(transaction)
	result, err := recordstore.UpdateReviewMultidiscAttachments(ctx, transaction, recordstore.Update{
		Set: `state='CANCELLED',error_code='CANCELLED',
diagnostics_json='{"errorCode":"CANCELLED","schemaVersion":1}',finished_at_ms=?,
version=version+1,updated_at_ms=?`,
		Scope:  recordstore.Scope{Where: `id=? AND state='RUNNING'`, Args: []any{write.Target.AttachmentID}},
		Values: []any{write.NowMS, write.NowMS},
	})
	if err := requireMultiDiscChange(result, err, "cancel attachment"); err != nil {
		return false, err
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='CANCELLED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
version=version+1,updated_at_ms=? WHERE id=? AND state='CANCEL_REQUESTED' AND worker_id=?
`, write.NowMS, write.NowMS, write.Target.JobID, write.Target.WorkerID)
	if err := requireMultiDiscChange(result, err, "cancel multi-disc job"); err != nil {
		return false, err
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'CANCELLED',?,?)
`, write.Target.JobID, write.Target.ItemID, write.EventJSON, write.NowMS); err != nil {
		return false, fmt.Errorf("record cancelled multi-disc event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit cancelled multi-disc attachment: %w", err)
	}
	return true, nil
}

func multiDiscAttachmentRetryDelay(attempt int64) time.Duration {
	delay := 250 * time.Millisecond
	for current := int64(1); current < attempt && delay < 4*time.Second; current++ {
		delay *= 2
	}
	return delay
}
