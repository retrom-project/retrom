package libraryimport

import (
	"context"

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
