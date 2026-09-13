package maintenance

import (
	"context"
	"fmt"

	"retrom/internal/repo/recordstore"
)

func (writes writes) StopBulkApprovals(ctx context.Context, nowMS int64) error {
	transaction := writes.transaction
	if _, err := recordstore.UpdateReviewBulkApprovalItems(ctx, transaction, recordstore.Update{
		Set: `
state='CANCELLED',outcome_code='RESTORE_INTERRUPTED',
outcome_details_json=json_object('schemaVersion',1,'code','RESTORE_INTERRUPTED'),completed_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `
bulk_approval_id IN (
  SELECT id FROM review_bulk_approvals WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')
) AND state IN ('PENDING','RUNNING')
`,
		},
		Values: []any{nowMS},
	}); err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored review bulk items: %w", err)
	}
	if _, err := recordstore.UpdateReviewBulkApprovals(ctx, transaction, recordstore.Update{
		Set: `
state='FAILED',last_error_code='RESTORE_INTERRUPTED',
processed_count=candidate_count,
published_count=(SELECT count(*) FROM review_bulk_approval_items item
  WHERE item.bulk_approval_id=review_bulk_approvals.id AND item.state='PUBLISHED'),
skipped_duplicate_count=(SELECT count(*) FROM review_bulk_approval_items item
  WHERE item.bulk_approval_id=review_bulk_approvals.id AND item.state='SKIPPED_DUPLICATE'),
skipped_changed_count=(SELECT count(*) FROM review_bulk_approval_items item
  WHERE item.bulk_approval_id=review_bulk_approvals.id AND item.state='SKIPPED_CHANGED'),
skipped_not_ready_count=(SELECT count(*) FROM review_bulk_approval_items item
  WHERE item.bulk_approval_id=review_bulk_approvals.id AND item.state='SKIPPED_NOT_READY'),
failed_count=(SELECT count(*) FROM review_bulk_approval_items item
  WHERE item.bulk_approval_id=review_bulk_approvals.id AND item.state='FAILED_FINAL'),
cancelled_count=(SELECT count(*) FROM review_bulk_approval_items item
  WHERE item.bulk_approval_id=review_bulk_approvals.id AND item.state='CANCELLED'),
cancel_requested_at_ms=NULL,cancel_reason=NULL,completed_at_ms=?,version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')`,
		},
		Values: []any{nowMS, nowMS},
	}); err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored review bulk approvals: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code='RESTORE_INTERRUPTED',error_retryable=0,
finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,
cancel_requested_at_ms=NULL,cancel_reason=NULL,version=version+1,updated_at_ms=?
WHERE kind='REVIEW_BULK_APPROVE' AND state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')
`, nowMS, nowMS); err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored review bulk jobs: %w", err)
	}
	return nil
}
