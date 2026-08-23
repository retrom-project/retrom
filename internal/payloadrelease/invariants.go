package payloadrelease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
)

var ErrLifecycleInvariant = errors.New("PAYLOAD_LIFECYCLE_INVARIANT")

func validateLifecycleState(ctx context.Context, database *sql.DB) error {
	transaction, err := database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("payloadrelease/validate lifecycle: %w", err)
	}
	defer cleanup.Rollback(transaction)
	checks := []string{
		`SELECT count(*) FROM import_items
WHERE state IN ('PUBLISHED','DISCARDED','FAILED_FINAL','CANCELLED')
  AND payload_state='RETAINED'`,
		`SELECT count(*) FROM import_jobs WHERE state IN ('COMPLETED','CANCELLED','FAILED') AND payload_state='RETAINED'`,
		`SELECT count(*) FROM pegasus_import_items WHERE payload_state='RETAINED' AND (
execution_state IN (
 'PUBLISHED','REVIEW_DISCARDED','SKIPPED_EXISTING','SKIPPED_MAPPING',
 'BLOCKED_SOURCE','BLOCKED_CONTENT','CANCELLED'
)
OR execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED') AND retryable=0)`,
		`SELECT count(*) FROM emulationstation_import_items WHERE payload_state='RETAINED' AND (
execution_state IN (
 'PUBLISHED','REVIEW_DISCARDED','SKIPPED_EXISTING','SKIPPED_MAPPING',
 'BLOCKED_SOURCE','BLOCKED_CONTENT','CANCELLED'
)
OR execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED') AND retryable=0)`,
		`SELECT count(*) FROM games WHERE status='DELETED' AND payload_state='RETAINED'`,
		`SELECT count(*) FROM (
SELECT payload_release_job_id FROM import_items WHERE payload_state<>'RETAINED'
UNION ALL SELECT payload_release_job_id FROM import_jobs WHERE payload_state<>'RETAINED'
UNION ALL SELECT payload_release_job_id FROM pegasus_import_items WHERE payload_state<>'RETAINED'
UNION ALL SELECT payload_release_job_id FROM emulationstation_import_items WHERE payload_state<>'RETAINED'
UNION ALL SELECT payload_release_job_id FROM games WHERE payload_state<>'RETAINED'
) owner LEFT JOIN jobs job ON job.id=owner.payload_release_job_id WHERE job.id IS NULL`,
	}
	for _, query := range checks {
		var count int64
		if err := transaction.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return fmt.Errorf("payloadrelease/validate lifecycle query: %w", err)
		}
		if count != 0 {
			return fmt.Errorf("%w: %d invalid rows", ErrLifecycleInvariant, count)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("payloadrelease/validate lifecycle commit: %w", err)
	}
	return nil
}
