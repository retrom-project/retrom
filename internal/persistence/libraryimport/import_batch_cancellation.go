package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	importcleanup "retrom/internal/service/libraryimport/payloadpolicy"

	dbapi "retrom/internal/database"
	payloadpersistence "retrom/internal/persistence/libraryimport/itemrelease"
	"retrom/internal/persistence/recordstore"
	payloadservice "retrom/internal/service/cleanupjobs"
	libraryservice "retrom/internal/service/libraryimport"
)

type ImportBatchCancellations struct{ database dbapi.DB }

func NewImportBatchCancellations(database dbapi.DB) *ImportBatchCancellations {
	return &ImportBatchCancellations{database: database}
}

type importCancellationEvidence struct {
	state                                           string
	groupJobID, groupState                          sql.NullString
	version, running, queued, reviewPending, failed int64
}

func (repository *ImportBatchCancellations) Cancel(
	ctx context.Context, request libraryservice.ImportBatchCancellationRequest, now int64,
) (libraryservice.ImportBatchCancellationResult, error) {
	var result libraryservice.ImportBatchCancellationResult
	err := dbapi.RetryTransaction(ctx, repository.database, func(tx dbapi.Tx) error {
		var err error
		result, err = repository.cancelInScope(ctx, tx, request, now)
		return err
	})
	if err != nil {
		return libraryservice.ImportBatchCancellationResult{}, fmt.Errorf("Cancel transaction: %w", err)
	}
	return result, nil
}

func (repository *ImportBatchCancellations) cancelInScope(
	ctx context.Context, tx dbapi.Tx, request libraryservice.ImportBatchCancellationRequest, now int64,
) (libraryservice.ImportBatchCancellationResult, error) {
	evidence, err := loadImportCancellationEvidence(ctx, tx, request.ImportID, request.ExpectedVersion)
	if err != nil {
		return libraryservice.ImportBatchCancellationResult{}, err
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
		return libraryservice.ImportBatchCancellationResult{}, fmt.Errorf("cancel import items: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE import_jobs SET state=?,cancel_requested_at_ms=?,cancel_reason=?,
cancelled_item_count=cancelled_item_count+?+?+?,queued_item_count=queued_item_count-?,
review_pending_item_count=review_pending_item_count-?,failed_item_count=failed_item_count-?,
version=version+1,updated_at_ms=?,completed_at_ms=CASE WHEN ?='CANCELLED' THEN ?::bigint ELSE NULL END
WHERE id=?
`, state, now, request.Reason, evidence.queued, evidence.reviewPending, evidence.failed,
		evidence.queued, evidence.reviewPending, evidence.failed, now, state, now, request.ImportID); err != nil {
		return libraryservice.ImportBatchCancellationResult{}, fmt.Errorf("cancel import aggregate: %w", err)
	}
	if err := transitionImportGroupCancellation(ctx, tx, evidence, request.Reason, now); err != nil {
		return libraryservice.ImportBatchCancellationResult{}, err
	}
	if err := scheduleCancelledImportPayloads(ctx, tx, request.ImportID, now); err != nil {
		return libraryservice.ImportBatchCancellationResult{}, err
	}
	return libraryservice.ImportBatchCancellationResult{
		ImportID: request.ImportID, GroupJobID: evidence.groupJobID.String,
		State: state, Version: evidence.version + 1, Pending: pending,
	}, nil
}

func loadImportCancellationEvidence(
	ctx context.Context, tx dbapi.Tx, importID string, expectedVersion int64,
) (importCancellationEvidence, error) {
	var evidence importCancellationEvidence
	err := dbapi.QueryRowContext(ctx, tx, `
SELECT state,version,running_item_count,
(SELECT count(*) FROM import_items WHERE import_job_id=import_jobs.id AND state='QUEUED'),
(SELECT count(*) FROM import_items WHERE import_job_id=import_jobs.id AND state IN ('REVIEW_PENDING',
'PUBLISHING')),
(SELECT count(*) FROM import_items WHERE import_job_id=import_jobs.id AND state='FAILED_RETRYABLE'),
(SELECT id FROM jobs WHERE scope_type='IMPORT_GROUP' AND scope_id=import_jobs.id AND kind='IMPORT_GROUP'),
(SELECT state FROM jobs WHERE scope_type='IMPORT_GROUP' AND scope_id=import_jobs.id AND kind='IMPORT_GROUP')
FROM import_jobs WHERE id=?
`, importID).Scan(&evidence.state, &evidence.version, &evidence.running, &evidence.queued,
		&evidence.reviewPending, &evidence.failed, &evidence.groupJobID, &evidence.groupState)
	if err != nil || evidence.version != expectedVersion {
		return importCancellationEvidence{}, libraryservice.ErrInvalid
	}
	if evidence.state == "COMPLETED" || evidence.state == "CANCELLED" || evidence.state == "FAILED" {
		return importCancellationEvidence{}, libraryservice.ErrInvalid
	}
	return evidence, nil
}

func transitionImportGroupCancellation(
	ctx context.Context, tx dbapi.Tx, evidence importCancellationEvidence, reason string, now int64,
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
SELECT id,scope_type,scope_id,'CANCELLED',jsonb_build_object('schemaVersion',1,'executionNo',execution_no,
 'attempt',attempt_count,'reason',?)::text,? FROM jobs WHERE id=?
`, reason, now, evidence.groupJobID.String); err != nil {
			return fmt.Errorf("record import group cancellation: %w", err)
		}
		return nil
	}
	if evidence.groupState.String == "RUNNING" {
		if _, err := tx.ExecContext(ctx, `
UPDATE jobs SET state='CANCEL_REQUESTED',cancel_requested_at_ms=?,cancel_reason=?,version=version+1,
updated_at_ms=?
WHERE id=? AND state='RUNNING'
`, now, reason, now, evidence.groupJobID.String); err != nil {
			return fmt.Errorf("request import group cancellation: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,scope_type,scope_id,'CANCEL_REQUESTED',jsonb_build_object('schemaVersion',1,'executionNo',
 execution_no,'attempt',attempt_count,'reason',?)::text,? FROM jobs WHERE id=?
`, reason, now, evidence.groupJobID.String); err != nil {
			return fmt.Errorf("record import group cancellation request: %w", err)
		}
	}
	return nil
}

func scheduleCancelledImportPayloads(ctx context.Context, tx dbapi.Tx, importID string, now int64) error {
	ids, err := dbapi.QueryStrings(ctx, tx, `
SELECT id FROM import_items WHERE import_job_id=? AND state='CANCELLED' AND payload_state='RETAINED'
ORDER BY id`, importID)
	if err != nil {
		return fmt.Errorf("list cancelled import payloads: %w", err)
	}
	scheduler := payloadservice.NewScheduler(nil)
	for _, itemID := range ids {
		if err := closeReview(ctx, tx, itemID, now); err != nil {
			return err
		}
		if _, err := importcleanup.TerminalItem(ctx, scheduler, payloadpersistence.BindScheduling(tx), itemID,
			payloadservice.ReasonImportCancelled, now); err != nil {
			return fmt.Errorf("schedule cancelled import payload: %w", err)
		}
	}
	if _, err := importcleanup.TerminalImport(ctx, scheduler, payloadpersistence.BindScheduling(tx),
		importID, now); err != nil {
		return fmt.Errorf("schedule cancelled import aggregate: %w", err)
	}
	return nil
}

var _ libraryservice.ImportBatchCancellationRepository = (*ImportBatchCancellations)(nil)
