package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/dbexec"
	payloadpersistence "retrom/internal/persistence/payloadrelease"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/libraryimport"
	payloadservice "retrom/internal/service/payloadrelease"
)

type ImportBatchCancellations struct{ database *sql.DB }

func NewImportBatchCancellations(database *sql.DB) *ImportBatchCancellations {
	return &ImportBatchCancellations{database: database}
}

type importCancellationEvidence struct {
	state                                           string
	groupJobID, groupState                          sql.NullString
	version, running, queued, reviewPending, failed int64
}

func (repository *ImportBatchCancellations) Cancel(
	ctx context.Context, request application.ImportBatchCancellationRequest, now int64,
) (application.ImportBatchCancellationResult, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return application.ImportBatchCancellationResult{}, fmt.Errorf("begin import cancellation: %w", err)
	}
	defer dbexec.Rollback(tx)
	evidence, err := loadImportCancellationEvidence(ctx, tx, request.ImportID, request.ExpectedVersion)
	if err != nil {
		return application.ImportBatchCancellationResult{}, err
	}
	if request.PreserveReviews {
		evidence.reviewPending = 0
	}
	pending := evidence.running > 0 || evidence.groupState.String == "RUNNING" ||
		evidence.groupState.String == "CANCEL_REQUESTED"
	state := "CANCELLED"
	if pending {
		state = "CANCEL_REQUESTED"
	}
	if _, err := recordstore.UpdateImportItems(ctx, tx, recordstore.Update{
		Set: `state='CANCELLED',failed_stage=NULL,last_error_code=NULL,completed_at_ms=?,updated_at_ms=?,version=version+1`,
		Scope: recordstore.Scope{Where: `import_job_id=? AND state IN ('QUEUED','REVIEW_PENDING','FAILED_RETRYABLE')
AND (state<>'REVIEW_PENDING' OR ?=0)`, Args: []any{request.ImportID, request.PreserveReviews}},
		Values: []any{now, now},
	}); err != nil {
		return application.ImportBatchCancellationResult{}, fmt.Errorf("cancel import items: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE import_jobs SET state=?,cancel_requested_at_ms=?,cancel_reason=?,
cancelled_item_count=cancelled_item_count+?+?+?,queued_item_count=queued_item_count-?,
review_pending_item_count=review_pending_item_count-?,failed_item_count=failed_item_count-?,
version=version+1,updated_at_ms=?,completed_at_ms=CASE WHEN ?='CANCELLED' THEN ? ELSE NULL END
WHERE id=?
`, state, now, request.Reason, evidence.queued, evidence.reviewPending, evidence.failed,
		evidence.queued, evidence.reviewPending, evidence.failed, now, state, now, request.ImportID); err != nil {
		return application.ImportBatchCancellationResult{}, fmt.Errorf("cancel import aggregate: %w", err)
	}
	if err := transitionImportGroupCancellation(ctx, tx, evidence, request.Reason, now); err != nil {
		return application.ImportBatchCancellationResult{}, err
	}
	if err := scheduleCancelledImportPayloads(ctx, tx, request.ImportID, now); err != nil {
		return application.ImportBatchCancellationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.ImportBatchCancellationResult{}, fmt.Errorf("commit import cancellation: %w", err)
	}
	return application.ImportBatchCancellationResult{
		ImportID: request.ImportID, GroupJobID: evidence.groupJobID.String,
		State: state, Version: evidence.version + 1, Pending: pending,
	}, nil
}

func loadImportCancellationEvidence(
	ctx context.Context, tx *sql.Tx, importID string, expectedVersion int64,
) (importCancellationEvidence, error) {
	var evidence importCancellationEvidence
	err := tx.QueryRowContext(ctx, `
SELECT state,version,running_item_count,
(SELECT count(*) FROM import_items WHERE import_job_id=import_jobs.id AND state='QUEUED'),
(SELECT count(*) FROM import_items WHERE import_job_id=import_jobs.id AND state='REVIEW_PENDING'),
(SELECT count(*) FROM import_items WHERE import_job_id=import_jobs.id AND state='FAILED_RETRYABLE'),
(SELECT id FROM jobs WHERE scope_type='IMPORT_GROUP' AND scope_id=import_jobs.id AND kind='IMPORT_GROUP'),
(SELECT state FROM jobs WHERE scope_type='IMPORT_GROUP' AND scope_id=import_jobs.id AND kind='IMPORT_GROUP')
FROM import_jobs WHERE id=?
`, importID).Scan(&evidence.state, &evidence.version, &evidence.running, &evidence.queued,
		&evidence.reviewPending, &evidence.failed, &evidence.groupJobID, &evidence.groupState)
	if err != nil || evidence.version != expectedVersion {
		return importCancellationEvidence{}, application.ErrInvalid
	}
	if evidence.state == "COMPLETED" || evidence.state == "CANCELLED" || evidence.state == "FAILED" {
		return importCancellationEvidence{}, application.ErrInvalid
	}
	return evidence, nil
}

func transitionImportGroupCancellation(
	ctx context.Context, tx *sql.Tx, evidence importCancellationEvidence, reason string, now int64,
) error {
	if evidence.groupState.String == "QUEUED" || evidence.groupState.String == "FAILED" {
		if _, err := tx.ExecContext(ctx, `
UPDATE jobs SET state='CANCELLED',cancel_requested_at_ms=?,cancel_reason=?,finished_at_ms=?
,version=version+1,updated_at_ms=?
WHERE id=? AND state IN ('QUEUED','FAILED')
`, now, reason, now, now, evidence.groupJobID.String); err != nil {
			return fmt.Errorf("cancel import group job: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,scope_type,scope_id,'CANCELLED',json_object('schemaVersion',1,'executionNo',execution_no,
'attempt',attempt_count,'reason',?),? FROM jobs WHERE id=?
`, reason, now, evidence.groupJobID.String); err != nil {
			return fmt.Errorf("record import group cancellation: %w", err)
		}
		return nil
	}
	if evidence.groupState.String == "RUNNING" {
		if _, err := tx.ExecContext(ctx, `
UPDATE jobs SET state='CANCEL_REQUESTED',cancel_requested_at_ms=?,cancel_reason=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'
`, now, reason, now, evidence.groupJobID.String); err != nil {
			return fmt.Errorf("request import group cancellation: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,scope_type,scope_id,'CANCEL_REQUESTED',json_object('schemaVersion',1,'executionNo',execution_no,
'attempt',attempt_count,'reason',?),? FROM jobs WHERE id=?
`, reason, now, evidence.groupJobID.String); err != nil {
			return fmt.Errorf("record import group cancellation request: %w", err)
		}
	}
	return nil
}

func scheduleCancelledImportPayloads(ctx context.Context, tx *sql.Tx, importID string, now int64) error {
	ids, err := payloadpersistence.CollectScopeIDs(ctx, tx, `
SELECT id FROM import_items WHERE import_job_id=? AND state='CANCELLED' AND payload_state='RETAINED'
ORDER BY id`, importID)
	if err != nil {
		return fmt.Errorf("list cancelled import payloads: %w", err)
	}
	scheduler := payloadservice.NewScheduler(nil)
	for _, itemID := range ids {
		if _, err := scheduler.TerminalItem(ctx, payloadpersistence.BindScheduling(tx), itemID,
			payloadservice.ReasonImportCancelled, now); err != nil {
			return fmt.Errorf("schedule cancelled import payload: %w", err)
		}
	}
	if _, err := scheduler.TerminalImport(ctx, payloadpersistence.BindScheduling(tx), importID, now); err != nil {
		return fmt.Errorf("schedule cancelled import aggregate: %w", err)
	}
	return nil
}

var _ application.ImportBatchCancellationRepository = (*ImportBatchCancellations)(nil)
