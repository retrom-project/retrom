package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
)

type acceptedMultiDiscEvidence struct {
	sourceSnapshotID, validationID string
	now                            int64
}

func (service *Service) createAcceptedMultiDiscEvidence(
	ctx context.Context,
	transaction *sql.Tx,
	candidate *multiDiscAttachmentCandidate,
) (acceptedMultiDiscEvidence, error) {
	validationFiles, dependencyJSON, err := service.resolveMultiDiscAttachmentValidation(ctx, transaction, candidate)
	if err != nil {
		return acceptedMultiDiscEvidence{}, multiDiscAttachmentStoreError("resolve validation", err)
	}
	var baseRevision int
	if err := transaction.QueryRowContext(ctx, `
SELECT revision_no FROM import_item_source_snapshots WHERE id=? AND import_item_id=?
`, candidate.input.BaseSourceSnapshotID, candidate.input.ImportItemID).Scan(&baseRevision); err != nil {
		return acceptedMultiDiscEvidence{}, multiDiscAttachmentStoreError("read base revision", err)
	}
	newSnapshotID, _ := uuid.NewV7()
	validationID, _ := uuid.NewV7()
	evidence := acceptedMultiDiscEvidence{
		sourceSnapshotID: newSnapshotID.String(), validationID: validationID.String(),
		now: service.now().UnixMilli(),
	}
	if err := insertMultiDiscSourceSnapshot(
		ctx, transaction, *candidate, evidence.sourceSnapshotID, baseRevision+1, evidence.now,
	); err != nil {
		return acceptedMultiDiscEvidence{}, err
	}
	if err := insertMultiDiscValidation(
		ctx, transaction, candidate, evidence.sourceSnapshotID, evidence.validationID,
		dependencyJSON, validationFiles, evidence.now,
	); err != nil {
		return acceptedMultiDiscEvidence{}, err
	}
	return evidence, nil
}

func expectOneRow(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected rows: %w", err)
	}
	if changed != 1 {
		return ErrInvalid
	}
	return nil
}

func (service *Service) advanceAcceptedMultiDiscState(
	ctx context.Context,
	transaction *sql.Tx,
	candidate *multiDiscAttachmentCandidate,
	evidence acceptedMultiDiscEvidence,
) error {
	selectedValidation := any(nil)
	if candidate.validationStatus == "READY" {
		selectedValidation = evidence.validationID
	}
	if err := expectOneRow(transaction.ExecContext(ctx, `
UPDATE review_drafts SET effective_source_snapshot_id=?,selected_validation_id=?,
version=version+1,updated_at_ms=? WHERE id=? AND effective_source_snapshot_id=?
`, evidence.sourceSnapshotID, selectedValidation, evidence.now,
		candidate.input.ReviewDraftID, candidate.input.BaseSourceSnapshotID)); err != nil {
		return multiDiscAttachmentStoreError("advance review source", err)
	}
	if err := recordMultiDiscDuplicateEvidence(
		ctx, transaction, candidate.input.ImportItemID, candidate.input.TargetPlatformID, evidence.now,
	); err != nil {
		return err
	}
	diagnostics, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "discCount": len(candidate.resultEntries),
		"attachedFileCount": len(candidate.uploadFiles), "validationStatus": candidate.validationStatus,
		"durationMs": multiDiscAttachmentDurationMS(*candidate, evidence.now),
	})
	if err := expectOneRow(transaction.ExecContext(ctx, `
UPDATE review_multidisc_attachments SET state='ACCEPTED',result_source_snapshot_id=?,
result_validation_id=?,diagnostics_json=?,error_code=NULL,finished_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'
`, evidence.sourceSnapshotID, evidence.validationID, string(diagnostics), evidence.now, evidence.now,
		candidate.input.AttachmentID)); err != nil {
		return multiDiscAttachmentStoreError("accept attachment", err)
	}
	consumptionID, _ := uuid.NewV7()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO upload_consumptions(id,upload_session_id,upload_file_id,consumer_type,consumer_id,created_at_ms)
VALUES(?,?,NULL,'REVIEW_MULTI_DISC',?,?)
`, consumptionID.String(), candidate.input.UploadSessionID, candidate.input.AttachmentID, evidence.now); err != nil {
		return multiDiscAttachmentStoreError("consume upload", err)
	}
	if err := expectOneRow(transaction.ExecContext(ctx, `
UPDATE import_items SET version=version+1,updated_at_ms=?
WHERE id=? AND state='REVIEW_PENDING'
`, evidence.now, candidate.input.ImportItemID)); err != nil {
		return multiDiscAttachmentStoreError("advance import item", err)
	}
	return nil
}

func recordAcceptedMultiDiscReviewEvent(
	ctx context.Context,
	transaction *sql.Tx,
	candidate multiDiscAttachmentCandidate,
	evidence acceptedMultiDiscEvidence,
) error {
	eventID, _ := uuid.NewV7()
	eventEvidence, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "attachmentId": candidate.input.AttachmentID,
		"baseSourceSnapshotId":   candidate.input.BaseSourceSnapshotID,
		"resultSourceSnapshotId": evidence.sourceSnapshotID, "validationId": evidence.validationID,
		"validationStatus": candidate.validationStatus, "state": "ACCEPTED",
	})
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_events(id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,
before_json,after_json,diff_json,config_evidence_json,dat_evidence_json,provider_evidence_json,created_at_ms)
VALUES(?,?,'DISC_ATTACHMENT_ACCEPTED','USER',?,NULL,'{}',?,?,'{}','{}','{}',?)
`, eventID.String(), candidate.input.ImportItemID, candidate.input.RequestedByUserID,
		string(eventEvidence), string(eventEvidence), evidence.now); err != nil {
		return multiDiscAttachmentStoreError("record review event", err)
	}
	return nil
}

func completeAcceptedMultiDiscJob(
	ctx context.Context,
	transaction *sql.Tx,
	candidate multiDiscAttachmentCandidate,
	evidence acceptedMultiDiscEvidence,
) error {
	parserData, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "parserResultCode": "MATCHED", "discCount": len(candidate.resultEntries),
	})
	terminalData, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "state": "ACCEPTED", "validationStatus": candidate.validationStatus,
		"durationMs": multiDiscAttachmentDurationMS(candidate, evidence.now),
	})
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms) VALUES
(?,'IMPORT_ITEM',?,'PLAYLIST_PARSED',?,?),
(?,'IMPORT_ITEM',?,'DISC_SET_MATCHED',?,?),
(?,'IMPORT_ITEM',?,'SOURCE_SNAPSHOT_CREATED',?,?),
(?,'IMPORT_ITEM',?,'CORE_VALIDATION_COMPLETED',?,?),
(?,'IMPORT_ITEM',?,'SUCCEEDED',?,?)
`, candidate.jobID, candidate.input.ImportItemID, string(parserData), evidence.now,
		candidate.jobID, candidate.input.ImportItemID, string(parserData), evidence.now,
		candidate.jobID, candidate.input.ImportItemID,
		fmt.Sprintf(`{"sourceSnapshotId":%q}`, evidence.sourceSnapshotID), evidence.now,
		candidate.jobID, candidate.input.ImportItemID,
		fmt.Sprintf(`{"validationId":%q,"status":%q}`, evidence.validationID, candidate.validationStatus), evidence.now,
		candidate.jobID, candidate.input.ImportItemID, string(terminalData), evidence.now); err != nil {
		return multiDiscAttachmentStoreError("record job events", err)
	}
	if err := expectOneRow(transaction.ExecContext(ctx, `
UPDATE jobs SET state='SUCCEEDED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING' AND worker_id=?
`, evidence.now, evidence.now, candidate.jobID, candidate.workerID)); err != nil {
		return multiDiscAttachmentStoreError("complete job", err)
	}
	return nil
}

func (service *Service) commitAcceptedMultiDiscAttachment(
	ctx context.Context,
	candidate *multiDiscAttachmentCandidate,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return multiDiscAttachmentStoreError("begin accepted commit", err)
	}
	defer cleanup.Rollback(transaction)
	if _, err := currentMultiDiscAttachmentInput(ctx, transaction, *candidate); err != nil {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, err)
	}
	if err := verifyMultiDiscAttachmentOwnership(ctx, transaction, *candidate); err != nil {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, err)
	}
	evidence, err := service.createAcceptedMultiDiscEvidence(ctx, transaction, candidate)
	if err != nil {
		return err
	}
	if err := service.advanceAcceptedMultiDiscState(ctx, transaction, candidate, evidence); err != nil {
		return err
	}
	if err := recordAcceptedMultiDiscReviewEvent(ctx, transaction, *candidate, evidence); err != nil {
		return err
	}
	if err := completeAcceptedMultiDiscJob(ctx, transaction, *candidate, evidence); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return multiDiscAttachmentStoreError("commit accepted attachment", err)
	}
	return nil
}

func (service *Service) runMultiDiscAttachment(parent context.Context, jobID string) {
	ctx, cancel := context.WithTimeout(parent, multiDiscAttachmentDeadline)
	defer cancel()
	candidate, err := service.claimMultiDiscAttachment(ctx, jobID)
	if err != nil {
		return
	}
	if err := service.readAttachedMultiDiscBase(ctx, &candidate); err != nil {
		service.finishRejectedMultiDiscAttachment(ctx, candidate, MultiDiscAttachmentErrorInputStale, err)
		return
	}
	if err := service.readMultiDiscAttachmentUploads(ctx, &candidate); err != nil {
		service.finishRejectedMultiDiscAttachment(ctx, candidate, MultiDiscAttachmentErrorInputStale, err)
		return
	}
	if err := service.validateMultiDiscAttachmentContents(ctx, &candidate); err != nil {
		if service.finishMultiDiscAttachmentCancellation(ctx, candidate) {
			return
		}
		code := MultiDiscAttachmentErrorCode(err)
		if code == "" {
			service.finishRetryableMultiDiscAttachment(ctx, candidate, MultiDiscAttachmentErrorUnavailable, err)
			return
		}
		service.finishRejectedMultiDiscAttachment(ctx, candidate, code, err)
		return
	}
	if err := service.commitAcceptedMultiDiscAttachment(ctx, &candidate); err != nil {
		if service.finishMultiDiscAttachmentCancellation(ctx, candidate) {
			return
		}
		code := MultiDiscAttachmentErrorCode(err)
		if code == MultiDiscAttachmentErrorInputStale {
			service.finishRejectedMultiDiscAttachment(ctx, candidate, code, err)
			return
		}
		service.finishRetryableMultiDiscAttachment(ctx, candidate, MultiDiscAttachmentErrorUnavailable, err)
	}
}

func (service *Service) finishRejectedMultiDiscAttachment(
	ctx context.Context,
	candidate multiDiscAttachmentCandidate,
	code string,
	cause error,
) {
	now := service.now().UnixMilli()
	diagnostics, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "errorCode": code, "causeCode": MultiDiscAttachmentErrorCode(cause),
		"durationMs": multiDiscAttachmentDurationMS(candidate, now),
	})
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	result, err := transaction.ExecContext(ctx, `
UPDATE review_multidisc_attachments SET state='REJECTED',error_code=?,diagnostics_json=?,
finished_at_ms=?,version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING'
`, code, string(diagnostics), now, now, candidate.input.AttachmentID)
	if err != nil {
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code=?,error_retryable=0,finished_at_ms=?,
leased_until_ms=NULL,heartbeat_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, code, now, now, candidate.jobID, candidate.workerID)
	if err != nil {
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms) VALUES
(?,'IMPORT_ITEM',?,'DISC_SET_REJECTED',?,?),
(?,'IMPORT_ITEM',?,'FAILED',?,?)
`, candidate.jobID, candidate.input.ImportItemID, string(diagnostics), now,
		candidate.jobID, candidate.input.ImportItemID, string(diagnostics), now); err != nil {
		return
	}
	eventID, _ := uuid.NewV7()
	evidence, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "attachmentId": candidate.input.AttachmentID,
		"baseSourceSnapshotId": candidate.input.BaseSourceSnapshotID,
		"state":                "REJECTED", "errorCode": code,
	})
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_events(id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,
before_json,after_json,diff_json,config_evidence_json,dat_evidence_json,provider_evidence_json,created_at_ms)
VALUES(?,?,'DISC_ATTACHMENT_REJECTED','USER',?,NULL,'{}',?,?,'{}','{}','{}',?)
`, eventID.String(), candidate.input.ImportItemID, candidate.input.RequestedByUserID,
		string(evidence), string(evidence), now); err != nil {
		return
	}
	_ = transaction.Commit()
}

func (service *Service) finishRetryableMultiDiscAttachment(
	ctx context.Context,
	candidate multiDiscAttachmentCandidate,
	code string,
	_ error,
) {
	if service.scheduleMultiDiscAttachmentRetry(ctx, candidate, code) {
		return
	}
	now := service.now().UnixMilli()
	diagnosticsJSON, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "errorCode": code,
		"durationMs": multiDiscAttachmentDurationMS(candidate, now),
	})
	diagnostics := string(diagnosticsJSON)
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	result, err := transaction.ExecContext(ctx, `
UPDATE review_multidisc_attachments SET state='FAILED_RETRYABLE',error_code=?,diagnostics_json=?,
finished_at_ms=?,version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING'
`, code, diagnostics, now, now, candidate.input.AttachmentID)
	if err != nil {
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code=?,error_retryable=1,finished_at_ms=?,
leased_until_ms=NULL,heartbeat_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, code, now, now, candidate.jobID, candidate.workerID)
	if err != nil {
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'FAILED',?,?)
`, candidate.jobID, candidate.input.ImportItemID, diagnostics, now); err != nil {
		return
	}
	_ = transaction.Commit()
}

func multiDiscAttachmentRetryDelay(attempt int64) time.Duration {
	delay := 250 * time.Millisecond
	for current := int64(1); current < attempt && delay < 4*time.Second; current++ {
		delay *= 2
	}
	return delay
}

func (service *Service) scheduleMultiDiscAttachmentRetry(
	ctx context.Context,
	candidate multiDiscAttachmentCandidate,
	code string,
) bool {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return false
	}
	defer cleanup.Rollback(transaction)
	var attemptCount, maxAttempts, deadline int64
	if err := transaction.QueryRowContext(ctx, `
SELECT attempt_count,max_attempts,execution_deadline_at_ms
FROM jobs WHERE id=? AND state='RUNNING' AND worker_id=?
`, candidate.jobID, candidate.workerID).Scan(&attemptCount, &maxAttempts, &deadline); err != nil {
		return false
	}
	now := service.now().UnixMilli()
	delay := multiDiscAttachmentRetryDelay(attemptCount)
	availableAt := now + delay.Milliseconds()
	if attemptCount >= maxAttempts || availableAt >= deadline {
		return false
	}
	diagnostics := fmt.Sprintf(`{"errorCode":%q,"schemaVersion":1}`, code)
	if err := expectOneRow(transaction.ExecContext(ctx, `
UPDATE review_multidisc_attachments SET state='FAILED_RETRYABLE',error_code=?,diagnostics_json=?,
finished_at_ms=?,version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING'
`, code, diagnostics, now, now, candidate.input.AttachmentID)); err != nil {
		return false
	}
	if err := expectOneRow(transaction.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',available_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
worker_id=NULL,error_code=NULL,error_retryable=NULL,finished_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, availableAt, now, candidate.jobID, candidate.workerID)); err != nil {
		return false
	}
	eventJSON, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "attempt": attemptCount, "retryAtMs": availableAt,
		"durationMs": multiDiscAttachmentDurationMS(candidate, now), "errorCode": code,
	})
	event := string(eventJSON)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'RETRY_SCHEDULED',?,?)
`, candidate.jobID, candidate.input.ImportItemID, event, now); err != nil {
		return false
	}
	if err := transaction.Commit(); err != nil {
		return false
	}
	service.scheduleMultiDiscAttachmentRun(ctx, candidate.jobID, delay)
	return true
}

func (service *Service) SyncMultiDiscAttachmentCancellation(ctx context.Context, jobID string) {
	now := service.now().UnixMilli()
	_, _ = service.database.ExecContext(ctx, `
UPDATE review_multidisc_attachments SET state='CANCELLED',error_code='CANCELLED',
diagnostics_json='{"errorCode":"CANCELLED","schemaVersion":1}',finished_at_ms=?,
version=version+1,updated_at_ms=?
WHERE job_id=? AND state IN ('QUEUED','RUNNING','FAILED_RETRYABLE')
AND EXISTS(SELECT 1 FROM jobs WHERE id=? AND state='CANCELLED')
`, now, now, jobID, jobID)
}

func (service *Service) finishMultiDiscAttachmentCancellation(
	ctx context.Context,
	candidate multiDiscAttachmentCandidate,
) bool {
	var state string
	if err := service.database.QueryRowContext(
		ctx, `SELECT state FROM jobs WHERE id=? AND worker_id=?`, candidate.jobID, candidate.workerID,
	).Scan(&state); err != nil || state != "CANCEL_REQUESTED" {
		return false
	}
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return false
	}
	defer cleanup.Rollback(transaction)
	result, err := transaction.ExecContext(ctx, `
UPDATE review_multidisc_attachments SET state='CANCELLED',error_code='CANCELLED',
diagnostics_json='{"errorCode":"CANCELLED","schemaVersion":1}',finished_at_ms=?,
version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING'
`, now, now, candidate.input.AttachmentID)
	if err != nil {
		return false
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return false
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='CANCELLED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
version=version+1,updated_at_ms=? WHERE id=? AND state='CANCEL_REQUESTED' AND worker_id=?
`, now, now, candidate.jobID, candidate.workerID)
	if err != nil {
		return false
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return false
	}
	cancelledData, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "state": "CANCELLED",
		"durationMs": multiDiscAttachmentDurationMS(candidate, now),
	})
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'CANCELLED',?,?)
`, candidate.jobID, candidate.input.ImportItemID, string(cancelledData), now); err != nil {
		return false
	}
	return transaction.Commit() == nil
}
