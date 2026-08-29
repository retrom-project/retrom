package libraryimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
)

var errReviewBulkNotRunnable = errors.New("review bulk approval is not runnable")

type reviewBulkWork struct {
	bulkID, jobID, workerID, userID string
}

type reviewBulkWorkItem struct {
	itemID, validationID, sourceSnapshotID string
	reviewVersion                          int64
}

type ReviewBulkItemPage struct {
	Items      []ReviewBulkItemResult `json:"items"`
	NextCursor *string                `json:"nextCursor"`
}

func (service *Service) claimReviewBulk(ctx context.Context, bulkID string) (reviewBulkWork, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return reviewBulkWork{}, fmt.Errorf("libraryimport/review bulk claim: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var work reviewBulkWork
	var bulkState, jobState string
	if err := transaction.QueryRowContext(ctx, `
SELECT bulk.job_id,bulk.state,job.state,bulk.created_by_user_id
FROM review_bulk_approvals bulk JOIN jobs job ON job.id=bulk.job_id WHERE bulk.id=?
`, bulkID).Scan(&work.jobID, &bulkState, &jobState, &work.userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return reviewBulkWork{}, errReviewBulkNotRunnable
		}
		return reviewBulkWork{}, fmt.Errorf("libraryimport/review bulk claim: %w", err)
	}
	if bulkState != "QUEUED" || jobState != "QUEUED" {
		return reviewBulkWork{}, errReviewBulkNotRunnable
	}
	workerID, _ := uuid.NewV7()
	work.bulkID, work.workerID = bulkID, workerID.String()
	now := service.now().UnixMilli()
	deadline := now + int64(reviewBulkDeadline/time.Millisecond)
	result, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='RUNNING',attempt_count=attempt_count+1,worker_id=?,execution_started_at_ms=?,
execution_deadline_at_ms=?,leased_until_ms=?,heartbeat_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='QUEUED'
`, work.workerID, now, deadline, now+60_000, now, now, work.jobID)
	if err != nil {
		return reviewBulkWork{}, fmt.Errorf("libraryimport/review bulk claim: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return reviewBulkWork{}, errReviewBulkNotRunnable
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE review_bulk_approvals SET state='RUNNING',started_at_ms=COALESCE(started_at_ms,?),
version=version+1,updated_at_ms=? WHERE id=? AND state='QUEUED'
`, now, now, bulkID)
	if err != nil {
		return reviewBulkWork{}, fmt.Errorf("libraryimport/review bulk claim: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return reviewBulkWork{}, errReviewBulkNotRunnable
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'REVIEW_BULK_APPROVAL',?,'STARTED',json_object('candidateCount',
(SELECT candidate_count FROM review_bulk_approvals WHERE id=?)),?)
`, work.jobID, bulkID, bulkID, now); err != nil {
		return reviewBulkWork{}, fmt.Errorf("libraryimport/review bulk claim: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return reviewBulkWork{}, fmt.Errorf("libraryimport/review bulk claim: %w", err)
	}
	return work, nil
}

func (service *Service) claimReviewBulkItem(
	ctx context.Context,
	work reviewBulkWork,
) (reviewBulkWorkItem, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return reviewBulkWorkItem{}, fmt.Errorf("libraryimport/review bulk item claim: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var item reviewBulkWorkItem
	err = transaction.QueryRowContext(ctx, `
SELECT item.import_item_id,item.expected_review_version,item.expected_validation_id,item.expected_source_snapshot_id
FROM review_bulk_approval_items item
JOIN review_bulk_approvals bulk ON bulk.id=item.bulk_approval_id
JOIN jobs job ON job.id=bulk.job_id
WHERE item.bulk_approval_id=? AND item.state='PENDING'
AND bulk.state='RUNNING' AND job.state='RUNNING' AND job.worker_id=?
ORDER BY item.ordinal LIMIT 1
`, work.bulkID, work.workerID).Scan(
		&item.itemID, &item.reviewVersion, &item.validationID, &item.sourceSnapshotID,
	)
	if err != nil {
		return reviewBulkWorkItem{}, fmt.Errorf("libraryimport/review bulk item claim: %w", err)
	}
	now := service.now().UnixMilli()
	result, err := transaction.ExecContext(ctx, `
UPDATE review_bulk_approval_items SET state='RUNNING',started_at_ms=?
WHERE bulk_approval_id=? AND import_item_id=? AND state='PENDING'
`, now, work.bulkID, item.itemID)
	if err != nil {
		return reviewBulkWorkItem{}, fmt.Errorf("libraryimport/review bulk item claim: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return reviewBulkWorkItem{}, errReviewBulkNotRunnable
	}
	if err := transaction.Commit(); err != nil {
		return reviewBulkWorkItem{}, fmt.Errorf("libraryimport/review bulk item claim: %w", err)
	}
	return item, nil
}

func reviewBulkProgressEvent(
	ctx context.Context,
	transaction *sql.Tx,
	work reviewBulkWork,
	now int64,
) error {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT ?, 'REVIEW_BULK_APPROVAL', ?, 'PROGRESS',
json_object('processed',processed_count,'candidate',candidate_count,'published',published_count,
'skipped',skipped_duplicate_count+skipped_changed_count+skipped_not_ready_count,
'failed',failed_count,'cancelled',cancelled_count), ?
FROM review_bulk_approvals WHERE id=?
`, work.jobID, work.bulkID, now, work.bulkID)
	if err != nil {
		return fmt.Errorf("libraryimport/review bulk progress: %w", err)
	}
	return nil
}

func (service *Service) markReviewBulkPublished(
	ctx context.Context,
	transaction *sql.Tx,
	work reviewBulkWork,
	item reviewBulkWorkItem,
	approved Approved,
) error {
	now := service.now().UnixMilli()
	result, err := transaction.ExecContext(ctx, `
UPDATE review_bulk_approval_items SET state='PUBLISHED',game_id=?,review_event_id=?,
outcome_code='PUBLISHED',outcome_details_json=json_object('schemaVersion',1,'code','PUBLISHED'),completed_at_ms=?
WHERE bulk_approval_id=? AND import_item_id=? AND state='RUNNING'
`, approved.GameID, approved.EventID, now, work.bulkID, item.itemID)
	if err != nil {
		return fmt.Errorf("libraryimport/review bulk publish outcome: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrInvalid
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE review_bulk_approvals SET processed_count=processed_count+1,published_count=published_count+1,
version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING'
`, now, work.bulkID)
	if err != nil {
		return fmt.Errorf("libraryimport/review bulk publish outcome: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrInvalid
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET heartbeat_at_ms=?,leased_until_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, now, now+60_000, now, work.jobID, work.workerID)
	if err != nil {
		return fmt.Errorf("libraryimport/review bulk publish outcome: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrInvalid
	}
	return reviewBulkProgressEvent(ctx, transaction, work, now)
}

func validReviewBulkOutcome(state string) bool {
	switch state {
	case "SKIPPED_DUPLICATE", "SKIPPED_CHANGED", "SKIPPED_NOT_READY", "FAILED_FINAL":
		return true
	default:
		return false
	}
}

func (service *Service) completeReviewBulkItem(
	ctx context.Context,
	work reviewBulkWork,
	item reviewBulkWorkItem,
	state, code string,
) error {
	if !validReviewBulkOutcome(state) {
		return ErrInvalid
	}
	details, _ := json.Marshal(map[string]any{"schemaVersion": 1, "code": code})
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("libraryimport/review bulk outcome: %w", err)
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	result, err := transaction.ExecContext(ctx, `
UPDATE review_bulk_approval_items SET state=?,outcome_code=?,outcome_details_json=?,completed_at_ms=?
WHERE bulk_approval_id=? AND import_item_id=? AND state='RUNNING'
`, state, code, string(details), now, work.bulkID, item.itemID)
	if err != nil {
		return fmt.Errorf("libraryimport/review bulk outcome: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errReviewBulkNotRunnable
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE review_bulk_approvals SET processed_count=processed_count+1,
skipped_duplicate_count=skipped_duplicate_count+CASE WHEN ?='SKIPPED_DUPLICATE' THEN 1 ELSE 0 END,
skipped_changed_count=skipped_changed_count+CASE WHEN ?='SKIPPED_CHANGED' THEN 1 ELSE 0 END,
skipped_not_ready_count=skipped_not_ready_count+CASE WHEN ?='SKIPPED_NOT_READY' THEN 1 ELSE 0 END,
failed_count=failed_count+CASE WHEN ?='FAILED_FINAL' THEN 1 ELSE 0 END,
version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING'
`, state, state, state, state, now, work.bulkID)
	if err != nil {
		return fmt.Errorf("libraryimport/review bulk outcome: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errReviewBulkNotRunnable
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET heartbeat_at_ms=?,leased_until_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, now, now+60_000, now, work.jobID, work.workerID)
	if err != nil {
		return fmt.Errorf("libraryimport/review bulk outcome: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errReviewBulkNotRunnable
	}
	if err := reviewBulkProgressEvent(ctx, transaction, work, now); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("libraryimport/review bulk outcome: %w", err)
	}
	return nil
}

func (service *Service) reviewBulkItemStillFrozen(
	ctx context.Context,
	item reviewBulkWorkItem,
) bool {
	var state, sourceSnapshotID string
	var version int64
	var validationID, validationStatus sql.NullString
	err := service.database.QueryRowContext(ctx, `
SELECT import_item.state,draft.version,draft.effective_source_snapshot_id,validation.id,validation.status
FROM import_items import_item
JOIN review_drafts draft ON draft.import_item_id=import_item.id
JOIN platform_instances instance ON instance.id=draft.target_platform_instance_id
LEFT JOIN rpgmaker_review_profiles rpg_profile ON rpg_profile.review_draft_id=draft.id
LEFT JOIN core_artifacts artifact ON artifact.id=CASE
 WHEN instance.platform_id='rpgmaker' THEN rpg_profile.artifact_id ELSE (
 SELECT selected.id FROM core_artifacts selected
 WHERE selected.core_id=instance.default_core_id AND selected.selected_for_new_bindings=1
) END
LEFT JOIN import_item_core_validations validation ON validation.id=(
 SELECT candidate.id FROM import_item_core_validations candidate
 WHERE candidate.import_item_id=import_item.id
 AND candidate.source_snapshot_id=draft.effective_source_snapshot_id
 AND candidate.target_platform_instance_id=draft.target_platform_instance_id
 AND candidate.core_artifact_id=artifact.id
 ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1)
WHERE import_item.id=?
AND (import_item.review_handoff_kind='DIRECT' OR EXISTS(
 SELECT 1 FROM emulationstation_import_items reserved_source
 WHERE reserved_source.library_import_item_id=import_item.id
 AND reserved_source.execution_state='REVIEW_PENDING'
))
`, item.itemID).Scan(&state, &version, &sourceSnapshotID, &validationID, &validationStatus)
	return err == nil && state == "REVIEW_PENDING" && version == item.reviewVersion &&
		sourceSnapshotID == item.sourceSnapshotID && validationID.String == item.validationID &&
		validationStatus.String == "READY"
}

func (service *Service) processReviewBulkItem(
	ctx context.Context,
	work reviewBulkWork,
	item reviewBulkWorkItem,
) error {
	_, err := service.approveWithOptions(ctx, item.itemID, item.reviewVersion, ApprovalDecision{}, approvalOptions{
		strictReady: true, expectedValidationID: item.validationID,
		expectedSourceSnapshotID: item.sourceSnapshotID, bulkApprovalID: work.bulkID,
		beforeCommit: func(ctx context.Context, transaction *sql.Tx, approved Approved) error {
			return service.markReviewBulkPublished(ctx, transaction, work, item, approved)
		},
	})
	if err == nil {
		return nil
	}
	var duplicate *DuplicateConflict
	if errors.As(err, &duplicate) {
		return service.completeReviewBulkItem(
			ctx, work, item, "SKIPPED_DUPLICATE", "DUPLICATE_GAME_CONFIRMATION_REQUIRED",
		)
	}
	if errors.Is(err, ErrInvalid) {
		if service.reviewBulkItemStillFrozen(ctx, item) {
			return service.completeReviewBulkItem(ctx, work, item, "SKIPPED_NOT_READY", "REVIEW_NOT_STRICT_READY")
		}
		return service.completeReviewBulkItem(ctx, work, item, "SKIPPED_CHANGED", "REVIEW_INPUT_CHANGED")
	}
	return service.completeReviewBulkItem(ctx, work, item, "FAILED_FINAL", "REVIEW_BULK_ITEM_FAILED")
}

func (service *Service) finishReviewBulk(ctx context.Context, work reviewBulkWork) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("libraryimport/review bulk finish: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var pending, failed int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FILTER(WHERE state IN ('PENDING','RUNNING')),
       count(*) FILTER(WHERE state='FAILED_FINAL')
FROM review_bulk_approval_items WHERE bulk_approval_id=?
`, work.bulkID).Scan(&pending, &failed); err != nil || pending != 0 {
		return fmt.Errorf("libraryimport/review bulk finish: %w", errReviewBulkNotRunnable)
	}
	now := service.now().UnixMilli()
	state := "COMPLETED"
	if failed != 0 {
		state = "PARTIAL_FAILURE"
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE review_bulk_approvals SET state=?,completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'
`, state, now, now, work.bulkID)
	if err != nil {
		return fmt.Errorf("libraryimport/review bulk finish: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errReviewBulkNotRunnable
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='SUCCEEDED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=?,worker_id=NULL,
version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING' AND worker_id=?
`, now, now, now, work.jobID, work.workerID)
	if err != nil {
		return fmt.Errorf("libraryimport/review bulk finish: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errReviewBulkNotRunnable
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'REVIEW_BULK_APPROVAL',?,'SUCCEEDED',json_object('state',?),?)
`, work.jobID, work.bulkID, state, now); err != nil {
		return fmt.Errorf("libraryimport/review bulk finish: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("libraryimport/review bulk finish: %w", err)
	}
	return nil
}

func (service *Service) finalizeReviewBulkCancellation(ctx context.Context, bulkID string) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("libraryimport/review bulk cancel: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var jobID string
	var remaining int
	if err := transaction.QueryRowContext(ctx, `
SELECT bulk.job_id,count(item.import_item_id) FILTER(WHERE item.state IN ('PENDING','RUNNING'))
FROM review_bulk_approvals bulk
LEFT JOIN review_bulk_approval_items item ON item.bulk_approval_id=bulk.id
WHERE bulk.id=? AND bulk.state IN ('QUEUED','RUNNING','CANCEL_REQUESTED') GROUP BY bulk.id
`, bulkID).Scan(&jobID, &remaining); err != nil {
		return errReviewBulkNotRunnable
	}
	now := service.now().UnixMilli()
	result, err := transaction.ExecContext(ctx, `
UPDATE review_bulk_approval_items SET state='CANCELLED',outcome_code='CANCELLED',
outcome_details_json=json_object('schemaVersion',1,'code','CANCELLED'),completed_at_ms=?
WHERE bulk_approval_id=? AND state IN ('PENDING','RUNNING')
`, now, bulkID)
	if err != nil {
		return fmt.Errorf("libraryimport/review bulk cancel: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != int64(remaining) {
		return errReviewBulkNotRunnable
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE review_bulk_approvals SET state='CANCELLED',processed_count=processed_count+?,
cancelled_count=cancelled_count+?,cancel_requested_at_ms=COALESCE(cancel_requested_at_ms,?),
completed_at_ms=?,version=version+1,updated_at_ms=? WHERE id=?
`, remaining, remaining, now, now, now, bulkID)
	if err != nil {
		return fmt.Errorf("libraryimport/review bulk cancel: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errReviewBulkNotRunnable
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='CANCELLED',cancel_requested_at_ms=COALESCE(cancel_requested_at_ms,?),
finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,version=version+1,updated_at_ms=?
WHERE id=?
`, now, now, now, jobID)
	if err != nil {
		return fmt.Errorf("libraryimport/review bulk cancel: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errReviewBulkNotRunnable
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'REVIEW_BULK_APPROVAL',?,'CANCELLED',json_object('cancelled',?),?)
`, jobID, bulkID, remaining, now); err != nil {
		return fmt.Errorf("libraryimport/review bulk cancel: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("libraryimport/review bulk cancel: %w", err)
	}
	return nil
}

func (service *Service) reviewBulkCancellationRequested(ctx context.Context, work reviewBulkWork) bool {
	var state string
	err := service.database.QueryRowContext(ctx, `SELECT state FROM review_bulk_approvals WHERE id=?`, work.bulkID).
		Scan(&state)
	return err == nil && state == "CANCEL_REQUESTED"
}

func (service *Service) failReviewBulkWorker(ctx context.Context, work reviewBulkWork) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, `
UPDATE review_bulk_approval_items SET state='PENDING',started_at_ms=NULL
WHERE bulk_approval_id=? AND state='RUNNING'
`, work.bulkID); err != nil {
		return
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE review_bulk_approvals SET state='FAILED',last_error_code='REVIEW_BULK_WORKER_UNAVAILABLE',
completed_at_ms=?,version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING'
`, now, now, work.bulkID)
	if err != nil {
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code='REVIEW_BULK_WORKER_UNAVAILABLE',error_retryable=1,
finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, now, now, work.jobID, work.workerID)
	if err != nil {
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'REVIEW_BULK_APPROVAL',?,'FAILED',json_object('code','REVIEW_BULK_WORKER_UNAVAILABLE'),?)
`, work.jobID, work.bulkID, now); err != nil {
		return
	}
	_ = transaction.Commit()
}

func (service *Service) failQueuedReviewBulkWorker(ctx context.Context, bulkID string) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	var jobID string
	if err := transaction.QueryRowContext(ctx, `
SELECT job_id FROM review_bulk_approvals WHERE id=? AND state='QUEUED'
`, bulkID).Scan(&jobID); err != nil {
		return
	}
	now := service.now().UnixMilli()
	result, err := transaction.ExecContext(ctx, `
UPDATE review_bulk_approvals SET state='FAILED',last_error_code='REVIEW_BULK_WORKER_UNAVAILABLE',
completed_at_ms=?,version=version+1,updated_at_ms=? WHERE id=? AND state='QUEUED'
`, now, now, bulkID)
	if err != nil {
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code='REVIEW_BULK_WORKER_UNAVAILABLE',error_retryable=1,
finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='QUEUED'
`, now, now, jobID)
	if err != nil {
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'REVIEW_BULK_APPROVAL',?,'FAILED',json_object('code','REVIEW_BULK_WORKER_UNAVAILABLE'),?)
`, jobID, bulkID, now); err != nil {
		return
	}
	_ = transaction.Commit()
}

func (service *Service) runReviewBulkApproval(ctx context.Context, bulkID string) {
	work, err := service.claimReviewBulk(ctx, bulkID)
	if errors.Is(err, errReviewBulkNotRunnable) {
		return
	}
	if err != nil {
		service.failQueuedReviewBulkWorker(context.WithoutCancel(ctx), bulkID)
		return
	}
	ctx = authn.WithPrincipal(ctx, authn.Principal{UserID: work.userID, Role: "ADMIN"})
	for {
		if service.reviewBulkCancellationRequested(ctx, work) {
			_ = service.finalizeReviewBulkCancellation(ctx, work.bulkID)
			return
		}
		item, err := service.claimReviewBulkItem(ctx, work)
		if errors.Is(err, sql.ErrNoRows) {
			service.finishOrCancelReviewBulk(ctx, work)
			return
		}
		if err != nil {
			service.failReviewBulkWorker(ctx, work)
			return
		}
		if err := service.processReviewBulkItem(ctx, work, item); err != nil {
			service.cancelOrFailReviewBulk(ctx, work)
			return
		}
	}
}

func (service *Service) cancelOrFailReviewBulk(ctx context.Context, work reviewBulkWork) {
	if service.reviewBulkCancellationRequested(ctx, work) {
		_ = service.finalizeReviewBulkCancellation(ctx, work.bulkID)
		return
	}
	service.failReviewBulkWorker(ctx, work)
}

func (service *Service) finishOrCancelReviewBulk(ctx context.Context, work reviewBulkWork) {
	if service.reviewBulkCancellationRequested(ctx, work) {
		_ = service.finalizeReviewBulkCancellation(ctx, work.bulkID)
		return
	}
	if err := service.finishReviewBulk(ctx, work); err != nil {
		service.cancelOrFailReviewBulk(ctx, work)
	}
}

type resumableReviewBulk struct{ id, state string }

func reviewBulkResumableJobs(ctx context.Context, transaction *sql.Tx) ([]resumableReviewBulk, error) {
	rows, err := transaction.QueryContext(ctx, `
SELECT id,state FROM review_bulk_approvals WHERE state IN ('QUEUED','CANCEL_REQUESTED') ORDER BY created_at_ms,id
`)
	if err != nil {
		return nil, fmt.Errorf("libraryimport/review bulk resume query: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	values := make([]resumableReviewBulk, 0)
	for rows.Next() {
		var value resumableReviewBulk
		if err := rows.Scan(&value.id, &value.state); err != nil {
			return nil, fmt.Errorf("libraryimport/review bulk resume scan: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("libraryimport/review bulk resume rows: %w", err)
	}
	return values, nil
}

func (service *Service) ResumeReviewBulkJobs(ctx context.Context) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, `
UPDATE review_bulk_approval_items SET state='PENDING',started_at_ms=NULL
WHERE state='RUNNING' AND bulk_approval_id IN (
 SELECT id FROM review_bulk_approvals WHERE state='RUNNING'
)
`); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',worker_id=NULL,execution_started_at_ms=NULL,execution_deadline_at_ms=NULL,
leased_until_ms=NULL,heartbeat_at_ms=NULL,available_at_ms=?,version=version+1,updated_at_ms=?
WHERE kind='REVIEW_BULK_APPROVE' AND state='RUNNING'
`, now, now); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE review_bulk_approvals SET state='QUEUED',version=version+1,updated_at_ms=? WHERE state='RUNNING'
`, now); err != nil {
		return
	}
	values, err := reviewBulkResumableJobs(ctx, transaction)
	if err != nil {
		return
	}
	if err := transaction.Commit(); err != nil {
		return
	}
	for _, value := range values {
		if value.state == "CANCEL_REQUESTED" {
			_ = service.finalizeReviewBulkCancellation(context.WithoutCancel(ctx), value.id)
			continue
		}
		go service.runReviewBulkApproval(context.WithoutCancel(ctx), value.id)
	}
}

type reviewBulkCancelTarget struct {
	state, jobID, jobState string
}

func loadReviewBulkCancelTarget(
	ctx context.Context,
	transaction *sql.Tx,
	bulkID string,
	expectedVersion int64,
) (reviewBulkCancelTarget, error) {
	var target reviewBulkCancelTarget
	var version int64
	err := transaction.QueryRowContext(ctx, `
SELECT bulk.state,bulk.version,bulk.job_id,job.state FROM review_bulk_approvals bulk
JOIN jobs job ON job.id=bulk.job_id WHERE bulk.id=?
`, bulkID).Scan(&target.state, &version, &target.jobID, &target.jobState)
	if err != nil || version != expectedVersion || (target.state != "QUEUED" && target.state != "RUNNING") {
		return reviewBulkCancelTarget{}, ErrReviewBulkConflict
	}
	return target, nil
}

func requestReviewBulkCancellation(
	ctx context.Context,
	transaction *sql.Tx,
	target reviewBulkCancelTarget,
	bulkID, reason string,
	expectedVersion, now int64,
) error {
	result, err := transaction.ExecContext(ctx, `
UPDATE review_bulk_approvals SET state='CANCEL_REQUESTED',cancel_requested_at_ms=?,cancel_reason=?,
version=version+1,updated_at_ms=? WHERE id=? AND version=?
`, now, reason, now, bulkID, expectedVersion)
	if err != nil {
		return fmt.Errorf("libraryimport/review bulk cancel: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrReviewBulkConflict
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='CANCEL_REQUESTED',cancel_requested_at_ms=?,cancel_reason=?,version=version+1,updated_at_ms=?
WHERE id=? AND state=?
`, now, reason, now, target.jobID, target.jobState)
	if err != nil {
		return fmt.Errorf("libraryimport/review bulk cancel: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrReviewBulkConflict
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'REVIEW_BULK_APPROVAL',?,'CANCEL_REQUESTED',json_object('reason',?),?)
`, target.jobID, bulkID, reason, now); err != nil {
		return fmt.Errorf("libraryimport/review bulk cancel: %w", err)
	}
	return nil
}

func (service *Service) CancelReviewBulk(
	ctx context.Context,
	bulkID string,
	expectedVersion int64,
	reason string,
) (ReviewBulkSummary, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 500 {
		return ReviewBulkSummary{}, ErrReviewBulkConflict
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("libraryimport/review bulk cancel: %w", err)
	}
	defer cleanup.Rollback(transaction)
	target, err := loadReviewBulkCancelTarget(ctx, transaction, bulkID, expectedVersion)
	if err != nil {
		return ReviewBulkSummary{}, err
	}
	now := service.now().UnixMilli()
	if err := requestReviewBulkCancellation(
		ctx, transaction, target, bulkID, reason, expectedVersion, now,
	); err != nil {
		return ReviewBulkSummary{}, err
	}
	if err := transaction.Commit(); err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("libraryimport/review bulk cancel: %w", err)
	}
	if target.state == "QUEUED" {
		if err := service.finalizeReviewBulkCancellation(ctx, bulkID); err != nil {
			return ReviewBulkSummary{}, err
		}
	}
	return service.GetReviewBulk(ctx, bulkID)
}

type reviewBulkRetryTarget struct {
	jobID, payload string
	executionNo    int64
}

func loadReviewBulkRetryTarget(
	ctx context.Context,
	transaction *sql.Tx,
	bulkID string,
	expectedVersion int64,
) (reviewBulkRetryTarget, error) {
	var target reviewBulkRetryTarget
	var version int64
	var errorCode string
	var retryable sql.NullInt64
	err := transaction.QueryRowContext(ctx, `
SELECT bulk.job_id,bulk.version,bulk.last_error_code,job.payload_json,job.execution_no,job.error_retryable
FROM review_bulk_approvals bulk JOIN jobs job ON job.id=bulk.job_id
WHERE bulk.id=? AND bulk.state='FAILED' AND job.state='FAILED'
`, bulkID).Scan(&target.jobID, &version, &errorCode, &target.payload, &target.executionNo, &retryable)
	if err != nil || version != expectedVersion || errorCode != "REVIEW_BULK_WORKER_UNAVAILABLE" ||
		!retryable.Valid || retryable.Int64 != 1 {
		return reviewBulkRetryTarget{}, ErrReviewBulkConflict
	}
	return target, nil
}

func queueReviewBulkRetry(
	ctx context.Context,
	transaction *sql.Tx,
	target reviewBulkRetryTarget,
	bulkID string,
	expectedVersion, now int64,
) error {
	executionNo := target.executionNo + 1
	digest := sha256.Sum256([]byte(target.payload))
	result, err := transaction.ExecContext(ctx, `
UPDATE review_bulk_approvals SET state='QUEUED',last_error_code=NULL,completed_at_ms=NULL,
version=version+1,updated_at_ms=? WHERE id=? AND version=?
`, now, bulkID, expectedVersion)
	if err != nil {
		return fmt.Errorf("libraryimport/review bulk retry: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrReviewBulkConflict
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',execution_no=?,attempt_count=0,available_at_ms=?,execution_started_at_ms=NULL,
execution_deadline_at_ms=NULL,leased_until_ms=NULL,heartbeat_at_ms=NULL,finished_at_ms=NULL,worker_id=NULL,
error_code=NULL,error_retryable=NULL,version=version+1,updated_at_ms=? WHERE id=?
`, executionNo, now, now, target.jobID)
	if err != nil {
		return fmt.Errorf("libraryimport/review bulk retry: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrReviewBulkConflict
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms) VALUES(?,?,?,?,?)
`, target.jobID, executionNo, target.payload, hex.EncodeToString(digest[:]), now); err != nil {
		return fmt.Errorf("libraryimport/review bulk retry: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'REVIEW_BULK_APPROVAL',?,'MANUAL_RETRY',json_object('executionNo',?),?)
`, target.jobID, bulkID, executionNo, now); err != nil {
		return fmt.Errorf("libraryimport/review bulk retry: %w", err)
	}
	return nil
}

func (service *Service) RetryReviewBulk(
	ctx context.Context,
	bulkID string,
	expectedVersion int64,
) (ReviewBulkSummary, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("libraryimport/review bulk retry: %w", err)
	}
	defer cleanup.Rollback(transaction)
	target, err := loadReviewBulkRetryTarget(ctx, transaction, bulkID, expectedVersion)
	if err != nil {
		return ReviewBulkSummary{}, err
	}
	if _, hasActive, err := activeReviewBulkSummary(ctx, transaction); err != nil {
		return ReviewBulkSummary{}, err
	} else if hasActive {
		return ReviewBulkSummary{}, ErrReviewBulkActive
	}
	now := service.now().UnixMilli()
	if err := queueReviewBulkRetry(ctx, transaction, target, bulkID, expectedVersion, now); err != nil {
		return ReviewBulkSummary{}, err
	}
	if err := transaction.Commit(); err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("libraryimport/review bulk retry: %w", err)
	}
	go service.runReviewBulkApproval(context.WithoutCancel(ctx), bulkID)
	return service.GetReviewBulk(ctx, bulkID)
}

var reviewBulkItemOutcomes = map[string]struct{}{
	"PUBLISHED": {}, "SKIPPED_DUPLICATE": {}, "SKIPPED_CHANGED": {},
	"SKIPPED_NOT_READY": {}, "FAILED_FINAL": {}, "CANCELLED": {},
}

func reviewBulkItemQuery(
	bulkID, outcome, cursor string,
	limit int,
) (string, []any, error) {
	if _, err := uuid.Parse(bulkID); err != nil || limit < 1 || limit > 50 {
		return "", nil, ErrReviewBulkConflict
	}
	if outcome != "" {
		if _, valid := reviewBulkItemOutcomes[outcome]; !valid {
			return "", nil, ErrReviewBulkConflict
		}
	}
	ordinal := -1
	if cursor != "" {
		parsed, err := strconv.Atoi(cursor)
		if err != nil || parsed < 0 {
			return "", nil, ErrReviewBulkConflict
		}
		ordinal = parsed
	}
	query := `SELECT import_item_id,title_snapshot,target_platform_name_snapshot,state,game_id,
review_event_id,outcome_code,outcome_details_json,completed_at_ms,ordinal
FROM review_bulk_approval_items WHERE bulk_approval_id=? AND ordinal>?`
	arguments := []any{bulkID, ordinal}
	if outcome != "" {
		query += " AND state=?"
		arguments = append(arguments, outcome)
	}
	query += " ORDER BY ordinal LIMIT ?"
	return query, append(arguments, limit+1), nil
}

type projectedReviewBulkItem struct {
	item    ReviewBulkItemResult
	ordinal int
}

func scanReviewBulkItems(rows *sql.Rows, limit int) (ReviewBulkItemPage, error) {
	projectedItems := make([]projectedReviewBulkItem, 0, limit+1)
	for rows.Next() {
		var value projectedReviewBulkItem
		var gameID, eventID, code, details sql.NullString
		var completed sql.NullInt64
		if err := rows.Scan(&value.item.ImportItemID, &value.item.Title, &value.item.PlatformName,
			&value.item.State, &gameID, &eventID, &code, &details, &completed, &value.ordinal); err != nil {
			return ReviewBulkItemPage{}, fmt.Errorf("libraryimport/review bulk items: %w", err)
		}
		value.item.GameID = nullableStringPointer(gameID)
		value.item.ReviewEventID = nullableStringPointer(eventID)
		value.item.OutcomeCode = nullableStringPointer(code)
		value.item.CompletedAtMS = nullableInt64Pointer(completed)
		if details.Valid {
			_ = json.Unmarshal([]byte(details.String), &value.item.OutcomeDetails)
		}
		projectedItems = append(projectedItems, value)
	}
	if err := rows.Err(); err != nil {
		return ReviewBulkItemPage{}, fmt.Errorf("libraryimport/review bulk item rows: %w", err)
	}
	page := ReviewBulkItemPage{Items: make([]ReviewBulkItemResult, 0, min(limit, len(projectedItems)))}
	for index, value := range projectedItems {
		if index == limit {
			next := strconv.Itoa(projectedItems[index-1].ordinal)
			page.NextCursor = &next
			break
		}
		page.Items = append(page.Items, value.item)
	}
	return page, nil
}

func (service *Service) ListReviewBulkItems(
	ctx context.Context,
	bulkID, outcome, cursor string,
	limit int,
) (ReviewBulkItemPage, error) {
	query, arguments, err := reviewBulkItemQuery(bulkID, outcome, cursor, limit)
	if err != nil {
		return ReviewBulkItemPage{}, err
	}
	rows, err := service.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return ReviewBulkItemPage{}, fmt.Errorf("libraryimport/review bulk items: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	return scanReviewBulkItems(rows, limit)
}
