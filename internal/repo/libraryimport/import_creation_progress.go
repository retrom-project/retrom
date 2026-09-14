package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/libraryimport"
)

func (records creationRecords) Aggregate(ctx context.Context, change application.CreationAggregate) error {
	result, err := records.transaction.ExecContext(
		ctx,
		`
UPDATE import_jobs SET state=?,running_item_count=?,review_pending_item_count=?,discarded_item_count=?,
 already_imported_item_count=?,already_imported_file_count=?,version=version+1,updated_at_ms=?,completed_at_ms=?
WHERE id=? AND version=? AND review_pending_item_count=?`,
		change.Projection.State,
		change.Running,
		change.Pending,
		change.Discarded,
		change.ImportedItems,
		change.ImportedFiles,
		change.NowMS,
		change.Projection.CompletedAtMS,
		change.ImportID,
		change.ExpectedVersion,
		change.ExpectedPending,
	)
	return creationMutation(result, err, "project creation aggregate", 1)
}

func (records creationRecords) FinishJob(ctx context.Context, change application.CreationJobFinish) error {
	before := change.Before
	work := before.Execution
	result, err := records.transaction.ExecContext(
		ctx,
		`
UPDATE jobs SET state='SUCCEEDED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
 worker_id=NULL,error_code=NULL,error_retryable=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND scope_type='IMPORT_GROUP' AND scope_id=? AND kind='IMPORT_GROUP' AND state='RUNNING'
 AND worker_id=? AND execution_no=? AND attempt_count=? AND execution_started_at_ms=?
AND execution_deadline_at_ms=? AND execution_deadline_at_ms>? AND leased_until_ms=? AND
 leased_until_ms>? AND version=?`,
		change.NowMS,
		change.NowMS,
		work.JobID,
		work.ImportID,
		work.WorkerID,
		work.ExecutionNo,
		work.Attempt,
		work.StartedAtMS,
		work.DeadlineMS,
		change.NowMS,
		before.LeaseUntilMS,
		change.NowMS,
		before.JobVersion,
	)
	return creationMutation(result, err, "finish queued creation execution", 1)
}

func (records creationRecords) Reconfiguration(
	ctx context.Context,
	id string,
) (application.CreationReconfigurationHead, error) {
	result := application.CreationReconfigurationHead{ImportID: id}
	result.Progress.Started = true
	counts := &result.Progress.Counts
	err := records.transaction.QueryRowContext(
		ctx,
		`
SELECT version,state,queued_item_count,running_item_count,review_pending_item_count,failed_item_count,
 cancelled_item_count,
rejected_file_count,resolved_rejected_file_count,cancel_requested_at_ms,completed_at_ms FROM
 import_jobs WHERE id=?`,
		id,
	).Scan(
		&result.Version,
		&result.Progress.State,
		&counts.Queued,
		&counts.Running,
		&counts.ReviewPending,
		&counts.Failed,
		&counts.Cancelled,
		&counts.Rejected,
		&counts.ResolvedRejected,
		&result.Progress.CancelRequestedAtMS,
		&result.Progress.CompletedAtMS,
	)
	if err != nil {
		return application.CreationReconfigurationHead{}, fmt.Errorf("query reconfiguration parent: %w", err)
	}
	rows, err := records.transaction.QueryContext(
		ctx,
		`
SELECT f.upload_file_id FROM import_job_files f
LEFT JOIN import_job_file_resolutions resolution ON resolution.import_job_id=f.import_job_id AND
 resolution.upload_file_id=f.upload_file_id
WHERE f.import_job_id=? AND f.disposition='REJECTED' AND resolution.upload_file_id IS NULL ORDER BY
 f.upload_file_id`,
		id,
	)
	if err != nil {
		return application.CreationReconfigurationHead{}, fmt.Errorf("query reconfiguration files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return application.CreationReconfigurationHead{}, fmt.Errorf("scan reconfiguration file: %w", err)
		}
		result.Files = append(result.Files, value)
	}
	if err := rows.Err(); err != nil {
		return application.CreationReconfigurationHead{}, fmt.Errorf("iterate reconfiguration files: %w", err)
	}
	return result, nil
}

func (records creationRecords) ResolveFiles(
	ctx context.Context,
	change application.CreationFileResolution,
) error {
	for _, id := range change.FileIDs {
		result, err := records.transaction.ExecContext(
			ctx,
			`
INSERT INTO import_job_file_resolutions(import_job_id,upload_file_id,action,replacement_import_job_id,
 actor_kind,actor_user_id,actor_label,created_at_ms)
SELECT f.import_job_id,f.upload_file_id,'RECONFIGURED',?,?,?,?,?
FROM import_job_files f LEFT JOIN import_job_file_resolutions resolution
ON resolution.import_job_id=f.import_job_id AND resolution.upload_file_id=f.upload_file_id
WHERE f.import_job_id=? AND f.upload_file_id=? AND f.disposition='REJECTED' AND
 resolution.upload_file_id IS NULL`,
			change.ReplacementID,
			change.Actor.Kind,
			change.Actor.UserID,
			change.Actor.Label,
			change.NowMS,
			change.Before.ImportID,
			id,
		)
		if err := creationMutation(result, err, "resolve creation rejected file", 1); err != nil {
			return err
		}
	}
	count := int64(len(change.FileIDs))
	result, err := records.transaction.ExecContext(
		ctx,
		`
UPDATE import_jobs SET resolved_rejected_file_count=resolved_rejected_file_count+?,state=?,
 completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND version=? AND state='PARTIAL_FAILURE' AND resolved_rejected_file_count=? AND
 resolved_rejected_file_count+?<=rejected_file_count`,
		count,
		change.Projection.State,
		change.Projection.CompletedAtMS,
		change.NowMS,
		change.Before.ImportID,
		change.Before.Version,
		change.Before.Progress.Counts.ResolvedRejected,
		count,
	)
	if err := creationMutation(result, err, "project reconfiguration aggregate", 1); err != nil {
		return err
	}
	result, err = records.transaction.ExecContext(
		ctx,
		`UPDATE import_jobs SET reconfigured_from_import_job_id=? WHERE id=? AND
 reconfigured_from_import_job_id IS NULL`,
		change.Before.ImportID,
		change.ReplacementID,
	)
	return creationMutation(result, err, "link reconfigured creation", 1)
}
