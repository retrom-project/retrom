package maintenance

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
)

func (writes writes) StopBulkApprovals(ctx context.Context, nowMS int64) error {
	transaction := writes.transaction
	if _, err := recordstore.UpdateReviewBulkApprovals(ctx, transaction, recordstore.Update{
		Set: `state='FAILED',last_error_code='RESTORE_INTERRUPTED',
completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope:  recordstore.Scope{Where: `state IN ('QUEUED','RUNNING')`},
		Values: []any{nowMS, nowMS},
	}); err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored review bulk: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code='RESTORE_INTERRUPTED',error_retryable=0,
finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,
version=version+1,updated_at_ms=?
WHERE kind='REVIEW_BULK_APPROVE' AND state IN ('QUEUED','RUNNING')`, nowMS, nowMS); err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored review bulk jobs: %w", err)
	}
	return nil
}
