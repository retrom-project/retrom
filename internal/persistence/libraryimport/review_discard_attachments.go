package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
)

func (records reviewDiscardRecords) CancelAttachments(ctx context.Context, itemID string, now int64) error {
	for _, table := range []string{"review_arcade_parent_attachments", "review_multidisc_attachments"} {
		if _, err := records.executor.ExecContext(ctx, `UPDATE jobs
SET state=CASE WHEN state='RUNNING' THEN 'CANCEL_REQUESTED' ELSE 'CANCELLED' END,
cancel_requested_at_ms=?,cancel_reason='review discarded',
finished_at_ms=CASE WHEN state='RUNNING' THEN NULL ELSE ? END,version=version+1,updated_at_ms=?
WHERE id IN (SELECT job_id FROM `+table+` WHERE import_item_id=? AND state='PENDING')
AND (state IN ('QUEUED','RUNNING') OR state='FAILED' AND error_retryable=1)`, now, now, now, itemID); err != nil {
			return fmt.Errorf("cancel discarded attachments: %w", err)
		}
		update := recordstore.Update{
			Set: `state='CANCELLED',error_code='CANCELLED',finished_at_ms=?,version=version+1,updated_at_ms=?`,
			Scope: recordstore.Scope{
				Where: `import_item_id=? AND state='PENDING' AND EXISTS(SELECT 1 FROM jobs job
WHERE job.id=` + table + `.job_id AND job.state='CANCELLED')`, Args: []any{itemID},
			},
			Values: []any{now, now},
		}

		var err error
		if table == "review_arcade_parent_attachments" {
			_, err = recordstore.UpdateReviewArcadeParentAttachments(ctx, records.executor, update)
		} else {
			_, err = recordstore.UpdateReviewMultidiscAttachments(ctx, records.executor, update)
		}
		if err != nil {
			return fmt.Errorf("cancel discarded attachment record: %w", err)
		}
	}
	return nil
}
