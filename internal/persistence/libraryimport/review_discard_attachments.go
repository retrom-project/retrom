package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
)

func (records reviewDiscardRecords) CancelAttachments(ctx context.Context, itemID string, now int64) error {
	transaction := records.transaction
	if _, err := transaction.ExecContext(ctx, `UPDATE jobs
SET state=CASE WHEN state='QUEUED' THEN 'CANCELLED' ELSE 'CANCEL_REQUESTED' END,
  cancel_requested_at_ms=?,cancel_reason='review discarded',
  finished_at_ms=CASE WHEN state='QUEUED' THEN ? ELSE NULL END,
  version=version+1,updated_at_ms=?
WHERE id IN (SELECT job_id FROM review_arcade_parent_attachments
  WHERE import_item_id=? AND state IN ('QUEUED','RUNNING'))
  AND state IN ('QUEUED','RUNNING')`, now, now, now, itemID); err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	if _, err := recordstore.UpdateReviewArcadeParentAttachments(ctx, transaction, recordstore.Update{
		Set: `
state='CANCELLED',error_code='CANCELLED',
  diagnostics_json='{"errorCode":"CANCELLED","schemaVersion":1}',finished_at_ms=?,
  version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `import_item_id=? AND state IN ('QUEUED','RUNNING')`,
			Args:  []any{itemID},
		},
		Values: []any{now, now},
	}); err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE jobs
SET state=CASE WHEN state='RUNNING' THEN 'CANCEL_REQUESTED' ELSE 'CANCELLED' END,
  cancel_requested_at_ms=?,cancel_reason='review discarded',
  finished_at_ms=CASE WHEN state='RUNNING' THEN NULL ELSE ? END,
  version=version+1,updated_at_ms=?
WHERE id IN (SELECT job_id FROM review_multidisc_attachments
  WHERE import_item_id=? AND state IN ('QUEUED','RUNNING','FAILED_RETRYABLE'))
  AND (state IN ('QUEUED','RUNNING') OR state='FAILED' AND error_retryable=1)`, now, now, now, itemID); err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	if _, err := recordstore.UpdateReviewMultidiscAttachments(ctx, transaction, recordstore.Update{
		Set: `
state='CANCELLED',error_code='CANCELLED',
  diagnostics_json='{"errorCode":"CANCELLED","schemaVersion":1}',finished_at_ms=?,
  version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `import_item_id=? AND state IN ('QUEUED','FAILED_RETRYABLE')`,
			Args:  []any{itemID},
		},
		Values: []any{now, now},
	}); err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	return nil
}
