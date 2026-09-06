package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/payloadrelease"
)

// DiscardBatchReviews records ordinary review decisions after execution has stopped.
// It processes a bounded group so large batches do not monopolize a worker.
func (service *Service) DiscardBatchReviews(ctx context.Context, importID string) (bool, error) {
	reviews, err := service.batchReviews(ctx, importID)
	if err != nil {
		return false, err
	}
	for _, item := range reviews {
		if _, err := service.discard(ctx, item.id, item.version, "丢弃本批次未发布内容", true); err != nil {
			return false, err
		}
	}
	return len(reviews) < 50, nil
}

type batchReview struct {
	id      string
	version int64
}

func (service *Service) batchReviews(ctx context.Context, importID string) ([]batchReview, error) {
	rows, err := service.database.QueryContext(ctx, `SELECT i.id,d.version
FROM import_items i JOIN review_drafts d ON d.import_item_id=i.id
WHERE i.import_job_id=? AND i.state='REVIEW_PENDING' ORDER BY i.id LIMIT 50`, importID)
	if err != nil {
		return nil, fmt.Errorf("libraryimport/list batch reviews: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	var reviews []batchReview
	for rows.Next() {
		var item batchReview
		if err := rows.Scan(&item.id, &item.version); err != nil {
			return nil, fmt.Errorf("libraryimport/read batch review: %w", err)
		}
		reviews = append(reviews, item)
	}
	iterationErr := rows.Err()
	if iterationErr != nil {
		return nil, fmt.Errorf("libraryimport/read batch reviews: %w", iterationErr)
	}
	return reviews, nil
}

// ReleaseDiscardedBatch closes rejected-only imports too. Published item ownership
// stays protected by Game references, independently of this import envelope.
func (service *Service) ReleaseDiscardedBatch(ctx context.Context, importID string) error {
	tx, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("libraryimport/finish batch discard: %w", err)
	}
	defer cleanup.Rollback(tx)
	now := service.now().UnixMilli()
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM import_items WHERE import_job_id=?
AND state NOT IN ('PUBLISHED','DISCARDED','FAILED_FINAL','CANCELLED')`, importID).Scan(&pending); err != nil {
		return fmt.Errorf("libraryimport/check batch discard: %w", err)
	}
	if pending != 0 {
		return ErrInvalid
	}
	if _, err := tx.ExecContext(ctx, `UPDATE import_jobs SET state='CANCELLED',cancel_reason='丢弃本批次未发布内容',
cancel_requested_at_ms=COALESCE(cancel_requested_at_ms,?),completed_at_ms=COALESCE(completed_at_ms,?),
updated_at_ms=?,version=version+1 WHERE id=? AND payload_state='RETAINED'`, now, now, now, importID); err != nil {
		return fmt.Errorf("libraryimport/close batch discard: %w", err)
	}
	ids, err := payloadrelease.CollectScopeIDs(ctx, tx, `
SELECT id FROM import_items WHERE import_job_id=? AND payload_state='RETAINED'`, importID)
	if err != nil {
		return fmt.Errorf("libraryimport/list discarded children: %w", err)
	}
	for _, id := range ids {
		if _, err := payloadrelease.ScheduleTerminalImportItem(
			ctx, tx, id, payloadrelease.ReasonImportDiscarded, now,
		); err != nil {
			return fmt.Errorf("libraryimport/release discarded child: %w", err)
		}
	}
	if _, err := payloadrelease.ScheduleTerminalImportJob(ctx, tx, importID, now); err != nil {
		return fmt.Errorf("libraryimport/release discarded batch: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("libraryimport/commit discarded batch: %w", err)
	}
	return nil
}
