package libraryimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"retrom/internal/dbexec"
	librarypersistence "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"

	"retrom/internal/persistence/recordstore"

	payloadpersistence "retrom/internal/persistence/payloadrelease"
	payloadservice "retrom/internal/service/payloadrelease"

	"github.com/google/uuid"
)

type DecisionResult = libraryservice.ReviewDecisionResult

func requireSingleReviewMutation(result sql.Result, err error, action string) error {
	if err != nil {
		return fmt.Errorf("libraryimport/review: %s: %w", action, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("libraryimport/review: %s result: %w", action, err)
	}
	if changed != 1 {
		return ErrInvalid
	}
	return nil
}

func (service *Service) reviewDiscards() *libraryservice.ReviewDiscards {
	return libraryservice.NewReviewDiscards(librarypersistence.NewReviewDiscards(service.database), service.now)
}

func (service *Service) Discard(
	ctx context.Context, itemID string, expectedVersion int64, reason string,
) (DecisionResult, error) {
	result, err := service.reviewDiscards().Discard(ctx, libraryservice.ReviewDiscardRequest{
		ItemID: itemID, ExpectedVersion: expectedVersion, Reason: reason, Mode: libraryservice.ReviewDiscardSingle,
	})
	if err != nil {
		return DecisionResult{}, fmt.Errorf("libraryimport/discard review: %w", err)
	}
	return result, nil
}

type RetryResult struct {
	ItemID  string `json:"itemId"`
	JobID   string `json:"jobId"`
	State   string `json:"state"`
	Version int64  `json:"version"`
}

// Retry eligibility, execution creation, event emission, and aggregate update share one transaction.
func (service *Service) RetryItem(ctx context.Context, itemID string, expectedVersion int64) (RetryResult, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return RetryResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	defer dbexec.Rollback(transaction)
	var importID, stage, manifestDigest string
	var version int64
	if err := transaction.QueryRowContext(ctx, `
SELECT import_job_id,
failed_stage,
source_manifest_digest,
version
FROM import_items
WHERE id=?
AND state='FAILED_RETRYABLE'
`, itemID).Scan(&importID, &stage, &manifestDigest, &version); err != nil ||
		version != expectedVersion {
		return RetryResult{}, ErrInvalid
	}
	jobID, _ := uuid.NewV7()
	now := service.now().UnixMilli()
	dedupe := sha256.Sum256([]byte(itemID + ":" + stage + ":" + time.UnixMilli(now).UTC().Format(time.RFC3339Nano)))
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(id,
scope_type,
scope_id,
kind,
dedupe_key,
execution_no,
payload_json,
cancellable,
state,
attempt_count,
max_attempts,
available_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
'IMPORT_ITEM',
?,
'IMPORT_ITEM_PIPELINE',
?,
1,
?,
1,
'QUEUED',
0,
2,
?,
?,
?)
`,
		jobID.String(),
		itemID,
		hex.EncodeToString(dedupe[:]),
		`{"sourceManifestDigest":"`+manifestDigest+`"}`,
		now,
		now,
		now,
	); err != nil {
		return RetryResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	itemResult, err := recordstore.UpdateImportItems(ctx, transaction, recordstore.Update{
		Set: `
state='QUEUED',
failed_stage=NULL,
last_error_code=NULL,
version=version+1,
updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=? AND state='FAILED_RETRYABLE'`,
			Args:  []any{itemID},
		},
		Values: []any{now},
	})
	if err := requireSingleReviewMutation(itemResult, err, "retry item"); err != nil {
		return RetryResult{}, err
	}
	jobResult, err := transaction.ExecContext(ctx, `
UPDATE import_jobs
SET failed_item_count=failed_item_count-1,
queued_item_count=queued_item_count+1,
state='RUNNING',
version=version+1,
updated_at_ms=?
WHERE id=? AND failed_item_count>0
`, now, importID)
	if err := requireSingleReviewMutation(jobResult, err, "retry job aggregate"); err != nil {
		return RetryResult{}, err
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'IMPORT_ITEM',
?,
'MANUAL_RETRY',
'{}',
?)
`, jobID.String(), itemID, now); err != nil {
		return RetryResult{}, fmt.Errorf("libraryimport/review: retry event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return RetryResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	return RetryResult{ItemID: itemID, JobID: jobID.String(), State: "QUEUED", Version: version + 1}, nil
}

type CancelResult struct {
	ImportJobID string `json:"importJobId"`
	State       string `json:"state"`
	Version     int64  `json:"version"`
}

type cancelImportEvidence struct {
	state                                           string
	groupJobID, groupState                          sql.NullString
	version, running, queued, reviewPending, failed int64
}

func (service *Service) Cancel(
	ctx context.Context,
	importID string,
	expectedVersion int64,
	reason string,
) (CancelResult, bool, error) {
	return service.cancelImport(ctx, importID, expectedVersion, reason, false)
}

// CancelForDiscard stops execution but leaves existing reviews for explicit discard events.
func (service *Service) CancelForDiscard(
	ctx context.Context, importID string, expectedVersion int64,
) (CancelResult, bool, error) {
	return service.cancelImport(ctx, importID, expectedVersion, "丢弃本批次未发布内容", true)
}

func (service *Service) cancelImport(
	ctx context.Context, importID string, expectedVersion int64, reason string, preserveReviews bool,
) (CancelResult, bool, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || !validField(reason, 500, true) {
		return CancelResult{}, false, ErrInvalid
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return CancelResult{}, false, fmt.Errorf("libraryimport/review: %w", err)
	}
	defer dbexec.Rollback(transaction)
	evidence, err := loadCancelImportEvidence(ctx, transaction, importID, expectedVersion)
	if err != nil {
		return CancelResult{}, false, err
	}
	if preserveReviews {
		evidence.reviewPending = 0
	}
	now := service.now().UnixMilli()
	pending := evidence.executionActive()
	newState := "CANCELLED"
	if pending {
		newState = "CANCEL_REQUESTED"
	}
	if _, err := recordstore.UpdateImportItems(ctx, transaction, recordstore.Update{
		Set: `
state='CANCELLED',
failed_stage=NULL,
last_error_code=NULL,
completed_at_ms=?,
updated_at_ms=?,
version=version+1
`,
		Scope: recordstore.Scope{
			Where: `
import_job_id=?
AND state IN ('QUEUED',
'REVIEW_PENDING',
'FAILED_RETRYABLE') AND (state<>'REVIEW_PENDING' OR ?=0)
`,
			Args: []any{importID, preserveReviews},
		},
		Values: []any{now, now},
	}); err != nil {
		return CancelResult{}, false, fmt.Errorf("libraryimport/review: cancel items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_jobs
SET state=?,
cancel_requested_at_ms=?,
cancel_reason=?,
cancelled_item_count=cancelled_item_count+?+?+?,
queued_item_count=queued_item_count-?,
review_pending_item_count=review_pending_item_count-?,
failed_item_count=failed_item_count-?,
version=version+1,
updated_at_ms=?,
completed_at_ms=CASE WHEN ?='CANCELLED' THEN ? ELSE NULL END
WHERE id=?
`, newState, now, reason,
		evidence.queued, evidence.reviewPending, evidence.failed,
		evidence.queued, evidence.reviewPending, evidence.failed,
		now, newState, now, importID); err != nil {
		return CancelResult{}, false, fmt.Errorf("libraryimport/review: cancel job aggregate: %w", err)
	}
	if err := transitionImportGroupCancellation(ctx, transaction, evidence, reason, now); err != nil {
		return CancelResult{}, false, err
	}
	if err := scheduleCancelledPayloads(ctx, transaction, importID, now); err != nil {
		return CancelResult{}, false, err
	}
	if err := transaction.Commit(); err != nil {
		return CancelResult{}, false, fmt.Errorf("libraryimport/review: %w", err)
	}
	if pending && evidence.groupJobID.Valid {
		service.CancelImportGroupJob(evidence.groupJobID.String)
	}
	return CancelResult{
		ImportJobID: importID, State: newState, Version: evidence.version + 1,
	}, pending, nil
}

func loadCancelImportEvidence(
	ctx context.Context,
	transaction *sql.Tx,
	importID string,
	expectedVersion int64,
) (cancelImportEvidence, error) {
	var evidence cancelImportEvidence
	err := transaction.QueryRowContext(ctx, `
SELECT state,
version,
running_item_count,
(SELECT count(*) FROM import_items WHERE import_job_id=import_jobs.id AND state='QUEUED'),
(SELECT count(*) FROM import_items WHERE import_job_id=import_jobs.id AND state='REVIEW_PENDING'),
(SELECT count(*) FROM import_items WHERE import_job_id=import_jobs.id AND state='FAILED_RETRYABLE'),
(SELECT id FROM jobs WHERE scope_type='IMPORT_GROUP' AND scope_id=import_jobs.id AND kind='IMPORT_GROUP'),
(SELECT state FROM jobs WHERE scope_type='IMPORT_GROUP' AND scope_id=import_jobs.id AND kind='IMPORT_GROUP')
FROM import_jobs
WHERE id=?
`, importID).Scan(
		&evidence.state, &evidence.version, &evidence.running, &evidence.queued,
		&evidence.reviewPending, &evidence.failed, &evidence.groupJobID, &evidence.groupState,
	)
	if err != nil || evidence.version != expectedVersion {
		return cancelImportEvidence{}, ErrInvalid
	}
	if evidence.state == "COMPLETED" || evidence.state == "CANCELLED" || evidence.state == "FAILED" {
		return cancelImportEvidence{}, ErrInvalid
	}
	return evidence, nil
}

func transitionImportGroupCancellation(
	ctx context.Context,
	transaction *sql.Tx,
	evidence cancelImportEvidence,
	reason string,
	now int64,
) error {
	if evidence.groupState.String == "QUEUED" || evidence.groupState.String == "FAILED" {
		if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='CANCELLED',cancel_requested_at_ms=?,cancel_reason=?,finished_at_ms=?,
 version=version+1,updated_at_ms=?
WHERE id=? AND state IN ('QUEUED','FAILED')
`, now, reason, now, now, evidence.groupJobID.String); err != nil {
			return fmt.Errorf("libraryimport/review: cancel group job: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,scope_type,scope_id,'CANCELLED',json_object('schemaVersion',1,
 'executionNo',execution_no,'attempt',attempt_count,'reason',?),?
FROM jobs WHERE id=?
`, reason, now, evidence.groupJobID.String); err != nil {
			return fmt.Errorf("libraryimport/review: record group cancellation: %w", err)
		}
		return nil
	}
	if evidence.groupState.String == "RUNNING" {
		if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='CANCEL_REQUESTED',cancel_requested_at_ms=?,cancel_reason=?,
 version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING'
`, now, reason, now, evidence.groupJobID.String); err != nil {
			return fmt.Errorf("libraryimport/review: request group cancellation: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,scope_type,scope_id,'CANCEL_REQUESTED',json_object('schemaVersion',1,
 'executionNo',execution_no,'attempt',attempt_count,'reason',?),?
FROM jobs WHERE id=?
`, reason, now, evidence.groupJobID.String); err != nil {
			return fmt.Errorf("libraryimport/review: record group cancellation request: %w", err)
		}
	}
	return nil
}

func scheduleCancelledPayloads(ctx context.Context, transaction *sql.Tx, importID string, now int64) error {
	itemIDs, err := payloadpersistence.CollectScopeIDs(ctx, transaction, `
SELECT id FROM import_items
WHERE import_job_id=? AND state='CANCELLED' AND payload_state='RETAINED'
ORDER BY id
`, importID)
	if err != nil {
		return fmt.Errorf("libraryimport/review: list cancelled payloads: %w", err)
	}
	for _, itemID := range itemIDs {
		if _, err := payloadservice.NewScheduler(nil).TerminalItem(
			ctx, payloadpersistence.BindScheduling(transaction), itemID, payloadservice.ReasonImportCancelled, now,
		); err != nil {
			return fmt.Errorf("libraryimport/review: schedule cancelled payload: %w", err)
		}
	}
	if _, err := payloadservice.NewScheduler(nil).TerminalImport(
		ctx, payloadpersistence.BindScheduling(transaction), importID, now,
	); err != nil {
		return fmt.Errorf("libraryimport/review: schedule cancelled aggregate: %w", err)
	}
	return nil
}

func (evidence cancelImportEvidence) executionActive() bool {
	return evidence.running > 0 || evidence.groupState.String == "RUNNING" ||
		evidence.groupState.String == "CANCEL_REQUESTED"
}
