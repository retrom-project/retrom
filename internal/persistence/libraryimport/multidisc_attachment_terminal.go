package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	libraryservice "retrom/internal/service/libraryimport"
)

var _ libraryservice.MultiDiscAttachmentTerminalRepository = (*MultiDiscAttachmentFinalization)(nil)

func (repository *MultiDiscAttachmentFinalization) Reject(
	ctx context.Context, write libraryservice.MultiDiscAttachmentRejectWrite,
) error {
	err := dbapi.RetryTransaction(ctx, repository.database, func(transaction dbapi.Tx) error {
		if err := requireAttachmentWorker(
			ctx, transaction, write.Target.JobID, write.Target.WorkerID, write.NowMS, false,
		); err != nil {
			return err
		}
		result, err := recordstore.UpdateReviewMultidiscAttachments(ctx, transaction, recordstore.Update{
			Set: `state='REJECTED',error_code=?,diagnostics_json=?,finished_at_ms=?,version=version+1,updated_at_ms=?`,
			Scope: recordstore.Scope{
				Where: `id=? AND state='PENDING'`, Args: []any{write.Target.AttachmentID},
			},
			Values: []any{write.Code, write.DiagnosticsJSON, write.NowMS, write.NowMS},
		})
		if err := requireAttachmentChange(result, err, "reject attachment"); err != nil {
			return err
		}
		result, err = transaction.ExecContext(ctx, `
	UPDATE jobs SET state='FAILED',error_code=?,error_retryable=0,finished_at_ms=?,
	leased_until_ms=NULL,heartbeat_at_ms=NULL,version=version+1,updated_at_ms=?
	WHERE id=? AND state='RUNNING' AND worker_id=?
	`, write.Code, write.NowMS, write.NowMS, write.Target.JobID, write.Target.WorkerID)
		if err := requireAttachmentChange(result, err, "fail rejected attachment job"); err != nil {
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

		return nil
	})
	if err != nil {
		return fmt.Errorf("commit rejected multi-disc attachment: %w", err)
	}
	return nil
}

func (repository *MultiDiscAttachmentFinalization) TryRetry(
	ctx context.Context, write libraryservice.MultiDiscAttachmentRetryWrite,
) (libraryservice.MultiDiscAttachmentRetryResult, error) {
	var result libraryservice.MultiDiscAttachmentRetryResult
	err := dbapi.RetryTransaction(ctx, repository.database, func(transaction dbapi.Tx) error {
		var err error
		result, err = repository.retryInScope(ctx, transaction, write)
		return err
	})
	if err != nil {
		return libraryservice.MultiDiscAttachmentRetryResult{}, fmt.Errorf("TryRetry transaction: %w", err)
	}
	return result, nil
}

func (repository *MultiDiscAttachmentFinalization) retryInScope(
	ctx context.Context, transaction dbapi.Tx, write libraryservice.MultiDiscAttachmentRetryWrite,
) (libraryservice.MultiDiscAttachmentRetryResult, error) {
	if err := requireAttachmentWorker(
		ctx, transaction, write.Target.JobID, write.Target.WorkerID, write.NowMS, false,
	); err != nil {
		return libraryservice.MultiDiscAttachmentRetryResult{}, err
	}
	var attemptCount, maxAttempts, deadline int64
	if err := dbapi.QueryRowContext(ctx, transaction, `
SELECT attempt_count,max_attempts,execution_deadline_at_ms
FROM jobs WHERE id=? AND state='RUNNING' AND worker_id=?
`, write.Target.JobID, write.Target.WorkerID).Scan(&attemptCount, &maxAttempts, &deadline); err != nil {
		return libraryservice.MultiDiscAttachmentRetryResult{}, fmt.Errorf("read multi-disc retry job: %w", err)
	}
	delay := multiDiscAttachmentRetryDelay(attemptCount)
	availableAt := write.NowMS + delay.Milliseconds()
	if attemptCount >= maxAttempts || availableAt >= deadline {
		return libraryservice.MultiDiscAttachmentRetryResult{}, nil
	}
	result, err := recordstore.UpdateReviewMultidiscAttachments(ctx, transaction, recordstore.Update{
		Set:    `diagnostics_json=?,version=version+1,updated_at_ms=?`,
		Scope:  recordstore.Scope{Where: `id=? AND state='PENDING'`, Args: []any{write.Target.AttachmentID}},
		Values: []any{write.DiagnosticsJSON, write.NowMS},
	})
	if err := requireAttachmentChange(result, err, "schedule retry attachment"); err != nil {
		return libraryservice.MultiDiscAttachmentRetryResult{}, err
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',available_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
worker_id=NULL,error_code=NULL,error_retryable=NULL,finished_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, availableAt, write.NowMS, write.Target.JobID, write.Target.WorkerID)
	if err := requireAttachmentChange(result, err, "queue multi-disc retry job"); err != nil {
		return libraryservice.MultiDiscAttachmentRetryResult{}, err
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
		return libraryservice.MultiDiscAttachmentRetryResult{}, fmt.Errorf("record multi-disc retry event: %w", err)
	}
	return libraryservice.MultiDiscAttachmentRetryResult{Scheduled: true, Delay: delay}, nil
}

func (repository *MultiDiscAttachmentFinalization) FailRetryable(
	ctx context.Context, write libraryservice.MultiDiscAttachmentRetryWrite,
) error {
	err := dbapi.RetryTransaction(ctx, repository.database, func(transaction dbapi.Tx) error {
		if err := requireAttachmentWorker(
			ctx, transaction, write.Target.JobID, write.Target.WorkerID, write.NowMS, false,
		); err != nil {
			return err
		}
		result, err := recordstore.UpdateReviewMultidiscAttachments(ctx, transaction, recordstore.Update{
			Set:    `diagnostics_json=?,version=version+1,updated_at_ms=?`,
			Scope:  recordstore.Scope{Where: `id=? AND state='PENDING'`, Args: []any{write.Target.AttachmentID}},
			Values: []any{write.DiagnosticsJSON, write.NowMS},
		})
		if err := requireAttachmentChange(result, err, "fail retryable attachment"); err != nil {
			return err
		}
		result, err = transaction.ExecContext(ctx, `
	UPDATE jobs SET state='FAILED',error_code=?,error_retryable=1,finished_at_ms=?,
	leased_until_ms=NULL,heartbeat_at_ms=NULL,version=version+1,updated_at_ms=?
	WHERE id=? AND state='RUNNING' AND worker_id=?
	`, write.Code, write.NowMS, write.NowMS, write.Target.JobID, write.Target.WorkerID)
		if err := requireAttachmentChange(result, err, "fail retryable attachment job"); err != nil {
			return err
		}
		if _, err := transaction.ExecContext(ctx, `
	INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
	VALUES(?,'IMPORT_ITEM',?,'FAILED',?,?)
	`, write.Target.JobID, write.Target.ItemID, write.DiagnosticsJSON, write.NowMS); err != nil {
			return fmt.Errorf("record failed multi-disc retry event: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit failed multi-disc retry: %w", err)
	}
	return nil
}

func (repository *MultiDiscAttachmentFinalization) FinishCancellation(
	ctx context.Context, write libraryservice.MultiDiscAttachmentCancellationWrite,
) (bool, error) {
	var result bool
	err := dbapi.RetryTransaction(ctx, repository.database, func(transaction dbapi.Tx) error {
		var err error
		result, err = repository.cancelInScope(ctx, transaction, write)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("FinishCancellation transaction: %w", err)
	}
	return result, nil
}

func (repository *MultiDiscAttachmentFinalization) cancelInScope(
	ctx context.Context, transaction dbapi.Tx, write libraryservice.MultiDiscAttachmentCancellationWrite,
) (bool, error) {
	var state string
	if err := dbapi.QueryRowContext(ctx, transaction,
		`SELECT state FROM jobs WHERE id=? AND worker_id=?`, write.Target.JobID, write.Target.WorkerID,
	).Scan(&state); err != nil {
		return false, fmt.Errorf("read multi-disc cancellation state: %w", err)
	}
	if state != "CANCEL_REQUESTED" {
		return false, nil
	}
	result, err := recordstore.UpdateReviewMultidiscAttachments(ctx, transaction, recordstore.Update{
		Set: `state='CANCELLED',error_code='CANCELLED',
diagnostics_json='{"errorCode":"CANCELLED","schemaVersion":1}',finished_at_ms=?,
version=version+1,updated_at_ms=?`,
		Scope:  recordstore.Scope{Where: `id=? AND state='PENDING'`, Args: []any{write.Target.AttachmentID}},
		Values: []any{write.NowMS, write.NowMS},
	})
	if err := requireAttachmentChange(result, err, "cancel attachment"); err != nil {
		return false, err
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='CANCELLED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
version=version+1,updated_at_ms=? WHERE id=? AND state='CANCEL_REQUESTED' AND worker_id=?
`, write.NowMS, write.NowMS, write.Target.JobID, write.Target.WorkerID)
	if err := requireAttachmentChange(result, err, "cancel multi-disc job"); err != nil {
		return false, err
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'CANCELLED',?,?)
`, write.Target.JobID, write.Target.ItemID, write.EventJSON, write.NowMS); err != nil {
		return false, fmt.Errorf("record cancelled multi-disc event: %w", err)
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
