package libraryimport

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"

	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/libraryimport"
)

func (records reviewApprovalRecords) RecordPublished(ctx context.Context, change application.BulkPublication) error {
	intent := change.Intent
	result, err := recordstore.UpdateReviewBulkApprovals(ctx, records.transaction, recordstore.Update{
		Set: `cursor_item_id=?,scanned_count=scanned_count+1,published_count=published_count+1,
version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND job_id=? AND state='RUNNING' AND max_item_id>=?
AND (cursor_item_id IS NULL OR cursor_item_id<?)
AND EXISTS(SELECT 1 FROM jobs WHERE id=? AND state='RUNNING' AND worker_id=?)`,
			Args: []any{
				intent.BulkID, intent.JobID, change.ItemID, change.ItemID,
				intent.JobID, intent.WorkerID,
			},
		},
		Values: []any{change.ItemID, change.NowMS},
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
	return nil
}

func (records reviewApprovalRecords) CheckRequest(ctx context.Context,
	request application.ReviewApprovalRequest, now int64,
) error {
	intent := request.Bulk
	var valid bool
	err := dbapi.QueryRowContext(ctx, records.transaction, `
SELECT EXISTS(SELECT 1 FROM review_bulk_approvals bulk JOIN jobs job ON job.id=bulk.job_id
 WHERE bulk.id=? AND job.id=? AND bulk.state='RUNNING' AND job.state='RUNNING' AND job.worker_id=? AND
job.leased_until_ms>?
 AND bulk.max_item_id>=? AND (bulk.cursor_item_id IS NULL OR bulk.cursor_item_id<?))
`, intent.BulkID, intent.JobID, intent.WorkerID, now, request.ItemID, request.ItemID).Scan(&valid)
	if err != nil {
		return fmt.Errorf("check request: %w", err)
	}
	if !valid {
		return application.ErrInvalid
	}
	return nil
}
