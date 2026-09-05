package payloadrelease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
)

type releaseError struct{ code string }

func (err releaseError) Error() string { return err.code }

func releaseFailure(code string) error { return releaseError{code: code} }

func errorCode(err error) string {
	var failure releaseError
	if errors.As(err, &failure) {
		return failure.code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "PAYLOAD_RELEASE_EXECUTION_TIMEOUT"
	}
	return "PAYLOAD_RELEASE_DATABASE_FAILED"
}

func (service *Service) finish(ctx context.Context, job claimedJob, executionErr error) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("payloadrelease/finish transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	if executionErr == nil {
		if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='SUCCEEDED',finished_at_ms=?,worker_id=NULL,leased_until_ms=NULL,heartbeat_at_ms=NULL,
error_code=NULL,error_retryable=NULL,version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING'
`, now, now, job.ID); err != nil {
			return fmt.Errorf("payloadrelease/finish success: %w", err)
		}
		if err := insertFinishEvent(ctx, transaction, job, "SUCCEEDED", `{"schemaVersion":1}`, now); err != nil {
			return err
		}
		if err := insertReleaseAudit(ctx, transaction, job, "PAYLOAD_RELEASE_COMPLETED", "RELEASED", "", now); err != nil {
			return err
		}
		return commitFinish(transaction)
	}
	code := errorCode(executionErr)
	if job.Attempt < 4 {
		available := now + releaseRetryDelay(job.Attempt).Milliseconds()
		if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',available_at_ms=?,worker_id=NULL,leased_until_ms=NULL,heartbeat_at_ms=NULL,
execution_started_at_ms=NULL,execution_deadline_at_ms=NULL,error_code=?,error_retryable=1,
version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING'
`, available, code, now, job.ID); err != nil {
			return fmt.Errorf("payloadrelease/schedule retry: %w", err)
		}
		data := fmt.Sprintf(`{"schemaVersion":1,"errorCode":%q,"attempt":%d}`, code, job.Attempt)
		if err := insertFinishEvent(ctx, transaction, job, "RETRY_SCHEDULED", data, now); err != nil {
			return err
		}
		return commitFinish(transaction)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',finished_at_ms=?,worker_id=NULL,leased_until_ms=NULL,heartbeat_at_ms=NULL,
error_code=?,error_retryable=1,version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING'
`, now, code, now, job.ID); err != nil {
		return fmt.Errorf("payloadrelease/finish failure: %w", err)
	}
	data := fmt.Sprintf(`{"schemaVersion":1,"errorCode":%q,"attempt":%d}`, code, job.Attempt)
	if err := insertFinishEvent(ctx, transaction, job, "FAILED", data, now); err != nil {
		return err
	}
	if err := markDomainFailedTx(ctx, transaction, job, code, now); err != nil {
		return err
	}
	if err := insertReleaseAudit(ctx, transaction, job, "PAYLOAD_RELEASE_FAILED", "FAILED", code, now); err != nil {
		return err
	}
	return commitFinish(transaction)
}

func releaseRetryDelay(attempt int64) time.Duration {
	switch attempt {
	case 1:
		return time.Second
	case 2:
		return 5 * time.Second
	default:
		return 30 * time.Second
	}
}

func insertReleaseAudit(
	ctx context.Context,
	transaction *sql.Tx,
	job claimedJob,
	action, state, code string,
	now int64,
) error {
	if job.Input.Kind != "PAYLOAD_RELEASE" {
		return nil
	}
	auditID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("payloadrelease/audit id: %w", err)
	}
	after := fmt.Sprintf(
		`{"schemaVersion":1,"jobId":%q,"scopeType":%q,"scopeId":%q,"state":%q}`,
		job.ID, job.ScopeType, job.ScopeID, state,
	)
	if code != "" {
		after = fmt.Sprintf(
			`{"schemaVersion":1,"jobId":%q,"scopeType":%q,"scopeId":%q,"state":%q,"errorCode":%q}`,
			job.ID, job.ScopeType, job.ScopeID, state, code,
		)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms)
VALUES(?,'SYSTEM',NULL,'payload-release-worker',?,?,?,NULL,?,NULL,NULL,?)
`, auditID.String(), action, job.ScopeType, job.ScopeID, after, now); err != nil {
		return fmt.Errorf("payloadrelease/audit: %w", err)
	}
	return nil
}

func insertFinishEvent(
	ctx context.Context,
	transaction *sql.Tx,
	job claimedJob,
	eventType, data string,
	now int64,
) error {
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,?,?,?,?,?)
`, job.ID, job.ScopeType, job.ScopeID, eventType, data, now); err != nil {
		return fmt.Errorf("payloadrelease/finish event: %w", err)
	}
	return nil
}

func markDomainFailedTx(ctx context.Context, transaction *sql.Tx, job claimedJob, code string, now int64) error {
	table := map[ScopeType]string{
		ScopeImportItem: "import_items", ScopeImportJob: "import_jobs",
		ScopePegasusImportItem:          "pegasus_import_items",
		ScopeEmulationStationImportItem: "emulationstation_import_items",
		ScopeGame:                       "games",
	}[job.ScopeType]
	if table == "" {
		return nil
	}
	update := `UPDATE ` + table + `
SET payload_state='FAILED',payload_last_error_code=?
WHERE id=? AND payload_release_job_id=?`
	arguments := []any{code, job.ScopeID, job.ID}
	if job.ScopeType == ScopeGame {
		update = `UPDATE games
SET payload_state='FAILED',payload_last_error_code=?,version=version+1,updated_at_ms=?
WHERE id=? AND payload_release_job_id=?`
		arguments = []any{code, now, job.ScopeID, job.ID}
	}
	_, err := transaction.ExecContext(ctx, update, arguments...)
	if err != nil {
		return fmt.Errorf("payloadrelease/mark domain failed: %w", err)
	}
	return nil
}

func commitFinish(transaction *sql.Tx) error {
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("payloadrelease/finish commit: %w", err)
	}
	return nil
}
