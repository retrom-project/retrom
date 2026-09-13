package libraryimport

import (
	"context"

	"retrom/internal/repo/recordstore"
	application "retrom/internal/service/libraryimport"
)

func (records reviewApprovalRecords) RecordPublished(ctx context.Context, change application.BulkPublication) error {
	intent := change.Intent
	result, err := recordstore.UpdateReviewBulkApprovalItems(ctx, records.transaction, recordstore.Update{
		Set: `state='PUBLISHED',game_id=?,review_event_id=?,outcome_code='PUBLISHED',
outcome_details_json='{"schemaVersion":1,"code":"PUBLISHED"}',completed_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `bulk_approval_id=? AND import_item_id=? AND state='RUNNING'
AND expected_review_version=? AND expected_validation_id=? AND expected_source_snapshot_id=?`,
			Args: []any{
				intent.BulkID, change.ItemID, change.ReviewVersion, intent.ValidationID,
				intent.SourceSnapshotID,
			},
		},
		Values: []any{change.Result.GameID, change.Result.EventID, change.NowMS},
	})
	if err := approvalMutation(result, err, "record bulk published item", true); err != nil {
		return err
	}
	result, err = recordstore.UpdateReviewBulkApprovals(ctx, records.transaction, recordstore.Update{
		Set: `processed_count=processed_count+1,published_count=published_count+1,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND job_id=? AND state='RUNNING'`,
			Args:  []any{intent.BulkID, intent.JobID},
		},
		Values: []any{change.NowMS},
	})
	if err := approvalMutation(result, err, "record bulk published aggregate", true); err != nil {
		return err
	}
	result, err = records.transaction.ExecContext(ctx, `
UPDATE jobs SET heartbeat_at_ms=?,leased_until_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?`,
		change.NowMS, change.LeasedUntilMS, change.NowMS, intent.JobID, intent.WorkerID)
	if err := approvalMutation(result, err, "renew bulk publication owner", true); err != nil {
		return err
	}
	result, err = records.transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT ?,'REVIEW_BULK_APPROVAL',?,'PROGRESS',
json_object('processed',processed_count,'candidate',candidate_count,'published',published_count,
 'skipped',skipped_duplicate_count+skipped_changed_count+skipped_not_ready_count,
 'failed',failed_count,'cancelled',cancelled_count),?
FROM review_bulk_approvals WHERE id=?`, intent.JobID, intent.BulkID, change.NowMS, intent.BulkID)
	return approvalMutation(result, err, "record bulk publication progress", true)
}
