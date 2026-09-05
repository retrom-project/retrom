package payloadrelease

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
)

func (service *Service) releaseExpiredReviewPreviews(ctx context.Context) error {
	for {
		count, err := service.releaseExpiredReviewPreviewBatch(ctx)
		if err != nil || count < 200 {
			return err
		}
	}
}

func (service *Service) releaseExpiredReviewPreviewBatch(ctx context.Context) (int, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("review preview expiry transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	ids, err := collectIDs(ctx, transaction, `
SELECT id FROM review_preview_sessions
WHERE (state='CREATED' AND bootstrap_expires_at_ms<=? OR hard_expires_at_ms<=? OR state='REVOKED')
 AND (state NOT IN ('EXPIRED','REVOKED') OR checkpoint_payload_blob_id IS NOT NULL
  OR restore_payload_blob_id IS NOT NULL)
ORDER BY hard_expires_at_ms,id LIMIT 200
`, now, now)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		blobs, err := collectIDs(ctx, transaction, `
SELECT checkpoint_payload_blob_id FROM review_preview_sessions WHERE id=?
UNION SELECT restore_payload_blob_id FROM review_preview_sessions WHERE id=?
`, id, id)
		if err != nil {
			return 0, err
		}
		if _, err := transaction.ExecContext(ctx, `
UPDATE review_preview_sessions
SET state=CASE WHEN state='REVOKED' THEN 'REVOKED' ELSE 'EXPIRED' END,
 finished_at_ms=COALESCE(finished_at_ms,?),updated_at_ms=?,version=version+1,
 checkpoint_payload_blob_id=NULL,checkpoint_format=NULL,checkpoint_created_at_ms=NULL,
 restore_payload_blob_id=NULL,restore_checkpoint_format=NULL
WHERE id=?
`, now, now, id); err != nil {
			return 0, fmt.Errorf("expire review preview: %w", err)
		}
		if err := service.stageCandidates(ctx, transaction, blobs); err != nil {
			return 0, err
		}
	}
	if err := transaction.Commit(); err != nil {
		return 0, fmt.Errorf("commit review preview expiry: %w", err)
	}
	return len(ids), nil
}
