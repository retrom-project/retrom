package libraryimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
	application "retrom/internal/service/libraryimport"
)

// ReviewBulkWorker owns the transaction boundary for one review bulk worker
// operation. The application layer supplies the orchestration and clock; this
// adapter owns the SQL and compare-and-set transitions.
type ReviewBulkWorker struct{ database *sql.DB }

var _ application.ReviewBulkWorkerRepository = (*ReviewBulkWorker)(nil)

func NewReviewBulkWorker(database *sql.DB) *ReviewBulkWorker {
	return &ReviewBulkWorker{database: database}
}

func (repository *ReviewBulkWorker) WithWorker(
	ctx context.Context,
	work func(application.ReviewBulkWorkerScope) error,
) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin review bulk worker transaction: %w", err)
	}
	defer dbexec.Rollback(transaction)
	if err := work(BindReviewBulkWorker(transaction)); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit review bulk worker transaction: %w", err)
	}
	return nil
}

type reviewBulkWorkerScope struct{ executor dbexec.Executor }

var _ application.ReviewBulkWorkerScope = reviewBulkWorkerScope{}

func BindReviewBulkWorker(executor dbexec.Executor) application.ReviewBulkWorkerScope {
	return reviewBulkWorkerScope{executor: executor}
}

func (scope reviewBulkWorkerScope) Claim(
	ctx context.Context,
	claim application.ReviewBulkClaim,
) (application.ReviewBulkWork, error) {
	var work application.ReviewBulkWork
	var bulkState, jobState string
	if err := scope.executor.QueryRowContext(ctx, `
SELECT bulk.job_id,bulk.state,job.state,bulk.created_by_user_id
FROM review_bulk_approvals bulk JOIN jobs job ON job.id=bulk.job_id WHERE bulk.id=?
`, claim.BulkApprovalID).Scan(&work.JobID, &bulkState, &jobState, &work.UserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.ReviewBulkWork{}, application.ErrReviewBulkWorkerNotRunnable
		}
		return application.ReviewBulkWork{}, fmt.Errorf("query review bulk claim: %w", err)
	}
	if bulkState != "QUEUED" || jobState != "QUEUED" {
		return application.ReviewBulkWork{}, application.ErrReviewBulkWorkerNotRunnable
	}
	work.BulkApprovalID = claim.BulkApprovalID
	work.WorkerID = claim.WorkerID
	result, err := scope.executor.ExecContext(ctx, `
UPDATE jobs SET state='RUNNING',attempt_count=attempt_count+1,worker_id=?,execution_started_at_ms=?,
execution_deadline_at_ms=?,leased_until_ms=?,heartbeat_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='QUEUED'
`, work.WorkerID, claim.NowMS, claim.DeadlineMS, claim.NowMS+60_000, claim.NowMS, claim.NowMS, work.JobID)
	if err != nil {
		return application.ReviewBulkWork{}, fmt.Errorf("update review bulk claim job: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ReviewBulkWork{}, application.ErrReviewBulkWorkerNotRunnable
	}
	result, err = recordstore.UpdateReviewBulkApprovals(ctx, scope.executor, recordstore.Update{
		Set: `
state='RUNNING',started_at_ms=COALESCE(started_at_ms,?),
version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=? AND state='QUEUED'`,
			Args:  []any{claim.BulkApprovalID},
		},
		Values: []any{claim.NowMS, claim.NowMS},
	})
	if err != nil {
		return application.ReviewBulkWork{}, fmt.Errorf("update review bulk claim approval: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ReviewBulkWork{}, application.ErrReviewBulkWorkerNotRunnable
	}
	if _, err := scope.executor.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'REVIEW_BULK_APPROVAL',?,'STARTED',json_object('candidateCount',
(SELECT candidate_count FROM review_bulk_approvals WHERE id=?)),?)
`, work.JobID, claim.BulkApprovalID, claim.BulkApprovalID, claim.NowMS); err != nil {
		return application.ReviewBulkWork{}, fmt.Errorf("insert review bulk claim event: %w", err)
	}
	return work, nil
}

func (scope reviewBulkWorkerScope) ClaimItem(
	ctx context.Context,
	work application.ReviewBulkWork,
	now int64,
) (application.ReviewBulkWorkItem, error) {
	var item application.ReviewBulkWorkItem
	err := scope.executor.QueryRowContext(ctx, `
SELECT item.import_item_id,item.expected_review_version,item.expected_validation_id,item.expected_source_snapshot_id
FROM review_bulk_approval_items item
JOIN review_bulk_approvals bulk ON bulk.id=item.bulk_approval_id
JOIN jobs job ON job.id=bulk.job_id
WHERE item.bulk_approval_id=? AND item.state='PENDING'
AND bulk.state='RUNNING' AND job.state='RUNNING' AND job.worker_id=?
ORDER BY item.ordinal LIMIT 1
`, work.BulkApprovalID, work.WorkerID).Scan(
		&item.ImportItemID, &item.ExpectedReviewVersion, &item.ValidationID, &item.SourceSnapshotID,
	)
	if err != nil {
		return application.ReviewBulkWorkItem{}, fmt.Errorf("query review bulk item claim: %w", err)
	}
	result, err := recordstore.UpdateReviewBulkApprovalItems(ctx, scope.executor, recordstore.Update{
		Set: `state='RUNNING',started_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `bulk_approval_id=? AND import_item_id=? AND state='PENDING'`,
			Args:  []any{work.BulkApprovalID, item.ImportItemID},
		},
		Values: []any{now},
	})
	if err != nil {
		return application.ReviewBulkWorkItem{}, fmt.Errorf("update review bulk item claim: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ReviewBulkWorkItem{}, application.ErrReviewBulkWorkerNotRunnable
	}
	return item, nil
}

func (scope reviewBulkWorkerScope) ProgressEvent(
	ctx context.Context,
	work application.ReviewBulkWork,
	now int64,
) error {
	_, err := scope.executor.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT ?, 'REVIEW_BULK_APPROVAL', ?, 'PROGRESS',
json_object('processed',processed_count,'candidate',candidate_count,'published',published_count,
'skipped',skipped_duplicate_count+skipped_changed_count+skipped_not_ready_count,
'failed',failed_count,'cancelled',cancelled_count), ?
FROM review_bulk_approvals WHERE id=?
`, work.JobID, work.BulkApprovalID, now, work.BulkApprovalID)
	if err != nil {
		return fmt.Errorf("insert review bulk progress event: %w", err)
	}
	return nil
}

func (scope reviewBulkWorkerScope) CompleteItem(
	ctx context.Context,
	completion application.ReviewBulkItemCompletion,
) error {
	result, err := recordstore.UpdateReviewBulkApprovalItems(ctx, scope.executor, recordstore.Update{
		Set: `state=?,outcome_code=?,outcome_details_json=?,completed_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `bulk_approval_id=? AND import_item_id=? AND state='RUNNING'`,
			Args:  []any{completion.Work.BulkApprovalID, completion.Item.ImportItemID},
		},
		Values: []any{
			completion.State, completion.OutcomeCode, completion.DetailsJSON, completion.NowMS,
		},
	})
	if err != nil {
		return fmt.Errorf("update review bulk item outcome: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ErrReviewBulkWorkerNotRunnable
	}
	result, err = recordstore.UpdateReviewBulkApprovals(ctx, scope.executor, recordstore.Update{
		Set: `
processed_count=processed_count+1,
skipped_duplicate_count=skipped_duplicate_count+CASE WHEN ?='SKIPPED_DUPLICATE' THEN 1 ELSE 0 END,
skipped_changed_count=skipped_changed_count+CASE WHEN ?='SKIPPED_CHANGED' THEN 1 ELSE 0 END,
skipped_not_ready_count=skipped_not_ready_count+CASE WHEN ?='SKIPPED_NOT_READY' THEN 1 ELSE 0 END,
failed_count=failed_count+CASE WHEN ?='FAILED_FINAL' THEN 1 ELSE 0 END,
version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=? AND state='RUNNING'`,
			Args:  []any{completion.Work.BulkApprovalID},
		},
		Values: []any{
			completion.State, completion.State, completion.State, completion.State, completion.NowMS,
		},
	})
	if err != nil {
		return fmt.Errorf("update review bulk outcome aggregate: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ErrReviewBulkWorkerNotRunnable
	}
	result, err = scope.executor.ExecContext(ctx, `
UPDATE jobs SET heartbeat_at_ms=?,leased_until_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, completion.NowMS, completion.LeasedUntilMS, completion.NowMS,
		completion.Work.JobID, completion.Work.WorkerID)
	if err != nil {
		return fmt.Errorf("renew review bulk outcome job: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ErrReviewBulkWorkerNotRunnable
	}
	return scope.ProgressEvent(ctx, completion.Work, completion.NowMS)
}

func (scope reviewBulkWorkerScope) ItemStillFrozen(
	ctx context.Context,
	item application.ReviewBulkWorkItem,
) (bool, error) {
	var state, sourceSnapshotID string
	var version int64
	var validationID, validationStatus sql.NullString
	err := scope.executor.QueryRowContext(ctx, `
SELECT import_item.state,draft.version,draft.effective_source_snapshot_id,validation.id,validation.status
FROM import_items import_item
JOIN review_drafts draft ON draft.import_item_id=import_item.id
LEFT JOIN import_item_core_validations validation ON validation.id=draft.selected_validation_id
WHERE import_item.id=?
AND (import_item.review_handoff_kind='DIRECT' OR EXISTS(
 SELECT 1 FROM emulationstation_import_items reserved_source
 WHERE reserved_source.library_import_item_id=import_item.id
 AND reserved_source.execution_state='REVIEW_PENDING'
))
	`, item.ImportItemID).Scan(&state, &version, &sourceSnapshotID, &validationID, &validationStatus)
	if err != nil {
		return false, fmt.Errorf("query frozen review bulk item: %w", err)
	}
	return state == "REVIEW_PENDING" && version == item.ExpectedReviewVersion &&
		sourceSnapshotID == item.SourceSnapshotID && validationID.String == item.ValidationID &&
		validationStatus.String == "READY", nil
}

func (scope reviewBulkWorkerScope) Finish(
	ctx context.Context,
	work application.ReviewBulkWork,
	now int64,
) error {
	var pending, failed int
	if err := scope.executor.QueryRowContext(ctx, `
SELECT count(*) FILTER(WHERE state IN ('PENDING','RUNNING')),
       count(*) FILTER(WHERE state='FAILED_FINAL')
FROM review_bulk_approval_items WHERE bulk_approval_id=?
`, work.BulkApprovalID).Scan(&pending, &failed); err != nil || pending != 0 {
		return application.ErrReviewBulkWorkerNotRunnable
	}
	state := "COMPLETED"
	if failed != 0 {
		state = "PARTIAL_FAILURE"
	}
	result, err := recordstore.UpdateReviewBulkApprovals(ctx, scope.executor, recordstore.Update{
		Set: `state=?,completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND state='RUNNING'`,
			Args:  []any{work.BulkApprovalID},
		},
		Values: []any{state, now, now},
	})
	if err != nil {
		return fmt.Errorf("update review bulk finished approval: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ErrReviewBulkWorkerNotRunnable
	}
	result, err = scope.executor.ExecContext(ctx, `
UPDATE jobs SET state='SUCCEEDED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=?,worker_id=NULL,
version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING' AND worker_id=?
`, now, now, now, work.JobID, work.WorkerID)
	if err != nil {
		return fmt.Errorf("update review bulk finished job: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ErrReviewBulkWorkerNotRunnable
	}
	if _, err := scope.executor.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'REVIEW_BULK_APPROVAL',?,'SUCCEEDED',json_object('state',?),?)
`, work.JobID, work.BulkApprovalID, state, now); err != nil {
		return fmt.Errorf("insert review bulk finished event: %w", err)
	}
	return nil
}

func (scope reviewBulkWorkerScope) FinalizeCancellation(
	ctx context.Context,
	bulkID string,
	now int64,
) error {
	var jobID string
	var remaining int
	if err := scope.executor.QueryRowContext(ctx, `
SELECT bulk.job_id,count(item.import_item_id) FILTER(WHERE item.state IN ('PENDING','RUNNING'))
FROM review_bulk_approvals bulk
LEFT JOIN review_bulk_approval_items item ON item.bulk_approval_id=bulk.id
WHERE bulk.id=? AND bulk.state IN ('QUEUED','RUNNING','CANCEL_REQUESTED') GROUP BY bulk.id
`, bulkID).Scan(&jobID, &remaining); err != nil {
		return application.ErrReviewBulkWorkerNotRunnable
	}
	result, err := recordstore.UpdateReviewBulkApprovalItems(ctx, scope.executor, recordstore.Update{
		Set: `
state='CANCELLED',outcome_code='CANCELLED',
outcome_details_json=json_object('schemaVersion',1,'code','CANCELLED'),completed_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `bulk_approval_id=? AND state IN ('PENDING','RUNNING')`,
			Args:  []any{bulkID},
		},
		Values: []any{now},
	})
	if err != nil {
		return fmt.Errorf("update cancelled review bulk items: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != int64(remaining) {
		return application.ErrReviewBulkWorkerNotRunnable
	}
	result, err = recordstore.UpdateReviewBulkApprovals(ctx, scope.executor, recordstore.Update{
		Set: `
state='CANCELLED',processed_count=processed_count+?,
cancelled_count=cancelled_count+?,cancel_requested_at_ms=COALESCE(cancel_requested_at_ms,?),
completed_at_ms=?,version=version+1,updated_at_ms=?
`,
		Scope:  recordstore.Scope{Where: `id=?`, Args: []any{bulkID}},
		Values: []any{remaining, remaining, now, now, now},
	})
	if err != nil {
		return fmt.Errorf("update cancelled review bulk approval: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ErrReviewBulkWorkerNotRunnable
	}
	result, err = scope.executor.ExecContext(ctx, `
UPDATE jobs SET state='CANCELLED',cancel_requested_at_ms=COALESCE(cancel_requested_at_ms,?),
finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,version=version+1,updated_at_ms=?
WHERE id=?
`, now, now, now, jobID)
	if err != nil {
		return fmt.Errorf("update cancelled review bulk job: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ErrReviewBulkWorkerNotRunnable
	}
	if _, err := scope.executor.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'REVIEW_BULK_APPROVAL',?,'CANCELLED',json_object('cancelled',?),?)
`, jobID, bulkID, remaining, now); err != nil {
		return fmt.Errorf("insert cancelled review bulk event: %w", err)
	}
	return nil
}

func (scope reviewBulkWorkerScope) CancellationRequested(
	ctx context.Context,
	bulkID string,
) (bool, error) {
	var state string
	if err := scope.executor.QueryRowContext(ctx,
		`SELECT state FROM review_bulk_approvals WHERE id=?`, bulkID).Scan(&state); err != nil {
		return false, fmt.Errorf("query review bulk cancellation state: %w", err)
	}
	return state == "CANCEL_REQUESTED", nil
}

func (scope reviewBulkWorkerScope) Fail(
	ctx context.Context,
	work application.ReviewBulkWork,
	now int64,
) error {
	if _, err := recordstore.UpdateReviewBulkApprovalItems(ctx, scope.executor, recordstore.Update{
		Set: `state='PENDING',started_at_ms=NULL`,
		Scope: recordstore.Scope{
			Where: `bulk_approval_id=? AND state='RUNNING'`,
			Args:  []any{work.BulkApprovalID},
		},
	}); err != nil {
		return fmt.Errorf("reset review bulk running items: %w", err)
	}
	result, err := recordstore.UpdateReviewBulkApprovals(ctx, scope.executor, recordstore.Update{
		Set: `
state='FAILED',last_error_code='REVIEW_BULK_WORKER_UNAVAILABLE',
completed_at_ms=?,version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=? AND state='RUNNING'`,
			Args:  []any{work.BulkApprovalID},
		},
		Values: []any{now, now},
	})
	if err != nil {
		return fmt.Errorf("update failed review bulk approval: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ErrReviewBulkWorkerNotRunnable
	}
	result, err = scope.executor.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code='REVIEW_BULK_WORKER_UNAVAILABLE',error_retryable=1,
finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, now, now, work.JobID, work.WorkerID)
	if err != nil {
		return fmt.Errorf("update failed review bulk job: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ErrReviewBulkWorkerNotRunnable
	}
	if _, err := scope.executor.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'REVIEW_BULK_APPROVAL',?,'FAILED',json_object('code','REVIEW_BULK_WORKER_UNAVAILABLE'),?)
`, work.JobID, work.BulkApprovalID, now); err != nil {
		return fmt.Errorf("insert failed review bulk event: %w", err)
	}
	return nil
}

func (scope reviewBulkWorkerScope) FailQueued(
	ctx context.Context,
	bulkID string,
	now int64,
) error {
	var jobID string
	if err := scope.executor.QueryRowContext(ctx, `
SELECT job_id FROM review_bulk_approvals WHERE id=? AND state='QUEUED'
`, bulkID).Scan(&jobID); err != nil {
		return fmt.Errorf("query queued review bulk job: %w", err)
	}
	result, err := recordstore.UpdateReviewBulkApprovals(ctx, scope.executor, recordstore.Update{
		Set: `
state='FAILED',last_error_code='REVIEW_BULK_WORKER_UNAVAILABLE',
completed_at_ms=?,version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=? AND state='QUEUED'`,
			Args:  []any{bulkID},
		},
		Values: []any{now, now},
	})
	if err != nil {
		return fmt.Errorf("update queued review bulk approval: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ErrReviewBulkWorkerNotRunnable
	}
	result, err = scope.executor.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code='REVIEW_BULK_WORKER_UNAVAILABLE',error_retryable=1,
finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='QUEUED'
`, now, now, jobID)
	if err != nil {
		return fmt.Errorf("update queued review bulk job: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ErrReviewBulkWorkerNotRunnable
	}
	if _, err := scope.executor.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'REVIEW_BULK_APPROVAL',?,'FAILED',json_object('code','REVIEW_BULK_WORKER_UNAVAILABLE'),?)
`, jobID, bulkID, now); err != nil {
		return fmt.Errorf("insert queued review bulk event: %w", err)
	}
	return nil
}

func (scope reviewBulkWorkerScope) Resume(
	ctx context.Context,
	now int64,
) ([]application.ReviewBulkResumableJob, error) {
	if _, err := recordstore.UpdateReviewBulkApprovalItems(ctx, scope.executor, recordstore.Update{
		Set: `state='PENDING',started_at_ms=NULL`,
		Scope: recordstore.Scope{Where: `
state='RUNNING' AND bulk_approval_id IN (
 SELECT id FROM review_bulk_approvals WHERE state='RUNNING'
)
`},
	}); err != nil {
		return nil, fmt.Errorf("reset interrupted review bulk items: %w", err)
	}
	if _, err := scope.executor.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',worker_id=NULL,execution_started_at_ms=NULL,execution_deadline_at_ms=NULL,
leased_until_ms=NULL,heartbeat_at_ms=NULL,available_at_ms=?,version=version+1,updated_at_ms=?
WHERE kind='REVIEW_BULK_APPROVE' AND state='RUNNING'
`, now, now); err != nil {
		return nil, fmt.Errorf("queue interrupted review bulk jobs: %w", err)
	}
	if _, err := recordstore.UpdateReviewBulkApprovals(ctx, scope.executor, recordstore.Update{
		Set:    `state='QUEUED',version=version+1,updated_at_ms=?`,
		Scope:  recordstore.Scope{Where: `state='RUNNING'`},
		Values: []any{now},
	}); err != nil {
		return nil, fmt.Errorf("queue interrupted review bulk approvals: %w", err)
	}
	return scope.ListResumable(ctx)
}

func (scope reviewBulkWorkerScope) ListResumable(
	ctx context.Context,
) ([]application.ReviewBulkResumableJob, error) {
	rows, err := scope.executor.QueryContext(ctx, `
SELECT id,state FROM review_bulk_approvals WHERE state IN ('QUEUED','CANCEL_REQUESTED') ORDER BY created_at_ms,id
`)
	if err != nil {
		return nil, fmt.Errorf("query resumable review bulk approvals: %w", err)
	}
	defer func() { cleanup.Error("close resumable review bulk approvals", rows.Close()) }()
	values := make([]application.ReviewBulkResumableJob, 0)
	for rows.Next() {
		var value application.ReviewBulkResumableJob
		if err := rows.Scan(&value.ID, &value.State); err != nil {
			return nil, fmt.Errorf("scan resumable review bulk approval: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate resumable review bulk approvals: %w", err)
	}
	return values, nil
}

func (scope reviewBulkWorkerScope) Active(ctx context.Context) (bool, error) {
	var found int
	err := scope.executor.QueryRowContext(ctx, `
SELECT 1 FROM review_bulk_approvals
WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED') LIMIT 1
`).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query active review bulk approval: %w", err)
	}
	return found == 1, nil
}

func (scope reviewBulkWorkerScope) LoadCancelTarget(
	ctx context.Context,
	bulkID string,
	expectedVersion int64,
) (application.ReviewBulkCancelTarget, error) {
	var target application.ReviewBulkCancelTarget
	var version int64
	err := scope.executor.QueryRowContext(ctx, `
SELECT bulk.state,bulk.version,bulk.job_id,job.state FROM review_bulk_approvals bulk
JOIN jobs job ON job.id=bulk.job_id WHERE bulk.id=?
`, bulkID).Scan(&target.State, &version, &target.JobID, &target.JobState)
	if err != nil || version != expectedVersion || (target.State != "QUEUED" && target.State != "RUNNING") {
		return application.ReviewBulkCancelTarget{}, application.ErrReviewBulkWorkerNotRunnable
	}
	return target, nil
}

func (scope reviewBulkWorkerScope) RequestCancellation(
	ctx context.Context,
	request application.ReviewBulkCancellationRequest,
) error {
	result, err := recordstore.UpdateReviewBulkApprovals(ctx, scope.executor, recordstore.Update{
		Set: `
state='CANCEL_REQUESTED',cancel_requested_at_ms=?,cancel_reason=?,
version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=? AND version=?`,
			Args:  []any{request.BulkApprovalID, request.ExpectedVersion},
		},
		Values: []any{request.NowMS, request.Reason, request.NowMS},
	})
	if err != nil {
		return fmt.Errorf("update review bulk cancellation: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ErrReviewBulkWorkerNotRunnable
	}
	result, err = scope.executor.ExecContext(ctx, `
UPDATE jobs SET state='CANCEL_REQUESTED',cancel_requested_at_ms=?,cancel_reason=?,version=version+1,updated_at_ms=?
WHERE id=? AND state=?
`, request.NowMS, request.Reason, request.NowMS, request.Target.JobID, request.Target.JobState)
	if err != nil {
		return fmt.Errorf("update review bulk cancellation job: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ErrReviewBulkWorkerNotRunnable
	}
	if _, err := scope.executor.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'REVIEW_BULK_APPROVAL',?,'CANCEL_REQUESTED',json_object('reason',?),?)
`, request.Target.JobID, request.BulkApprovalID, request.Reason, request.NowMS); err != nil {
		return fmt.Errorf("insert review bulk cancellation event: %w", err)
	}
	return nil
}

func (scope reviewBulkWorkerScope) LoadRetryTarget(
	ctx context.Context,
	bulkID string,
	expectedVersion int64,
) (application.ReviewBulkRetryTarget, error) {
	var target application.ReviewBulkRetryTarget
	var version int64
	var errorCode string
	var retryable sql.NullInt64
	err := scope.executor.QueryRowContext(ctx, `
SELECT bulk.job_id,bulk.version,bulk.last_error_code,job.payload_json,job.execution_no,job.error_retryable
FROM review_bulk_approvals bulk JOIN jobs job ON job.id=bulk.job_id
WHERE bulk.id=? AND bulk.state='FAILED' AND job.state='FAILED'
`, bulkID).Scan(&target.JobID, &version, &errorCode, &target.PayloadJSON, &target.ExecutionNo, &retryable)
	if err != nil || version != expectedVersion || errorCode != "REVIEW_BULK_WORKER_UNAVAILABLE" ||
		!retryable.Valid || retryable.Int64 != 1 {
		return application.ReviewBulkRetryTarget{}, application.ErrReviewBulkWorkerNotRunnable
	}
	return target, nil
}

func (scope reviewBulkWorkerScope) QueueRetry(
	ctx context.Context,
	request application.ReviewBulkRetryRequest,
) error {
	executionNo := request.Target.ExecutionNo + 1
	digest := sha256.Sum256([]byte(request.Target.PayloadJSON))
	result, err := recordstore.UpdateReviewBulkApprovals(ctx, scope.executor, recordstore.Update{
		Set: `
state='QUEUED',last_error_code=NULL,completed_at_ms=NULL,
version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=? AND version=?`,
			Args:  []any{request.BulkApprovalID, request.ExpectedVersion},
		},
		Values: []any{request.NowMS},
	})
	if err != nil {
		return fmt.Errorf("update review bulk retry approval: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ErrReviewBulkWorkerNotRunnable
	}
	result, err = scope.executor.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',execution_no=?,attempt_count=0,available_at_ms=?,execution_started_at_ms=NULL,
execution_deadline_at_ms=NULL,leased_until_ms=NULL,heartbeat_at_ms=NULL,finished_at_ms=NULL,worker_id=NULL,
error_code=NULL,error_retryable=NULL,version=version+1,updated_at_ms=? WHERE id=?
`, executionNo, request.NowMS, request.NowMS, request.Target.JobID)
	if err != nil {
		return fmt.Errorf("update review bulk retry job: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ErrReviewBulkWorkerNotRunnable
	}
	if _, err := scope.executor.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms) VALUES(?,?,?,?,?)
`, request.Target.JobID, executionNo, request.Target.PayloadJSON,
		hex.EncodeToString(digest[:]), request.NowMS); err != nil {
		return fmt.Errorf("insert review bulk retry input: %w", err)
	}
	if _, err := scope.executor.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'REVIEW_BULK_APPROVAL',?,'MANUAL_RETRY',json_object('executionNo',?),?)
`, request.Target.JobID, request.BulkApprovalID, executionNo, request.NowMS); err != nil {
		return fmt.Errorf("insert review bulk retry event: %w", err)
	}
	return nil
}
