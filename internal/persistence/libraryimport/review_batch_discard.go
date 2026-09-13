package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/persistence/dbexec"
	payloadpersistence "retrom/internal/persistence/payloadrelease"
	application "retrom/internal/service/libraryimport"
	payloadservice "retrom/internal/service/payloadrelease"
)

type ReviewBatchDiscards struct{ database *sql.DB }

func NewReviewBatchDiscards(database *sql.DB) *ReviewBatchDiscards {
	return &ReviewBatchDiscards{database: database}
}

func (repository *ReviewBatchDiscards) Pending(
	ctx context.Context, importID string, limit int,
) ([]application.ReviewBatchItem, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT i.id,d.version
FROM import_items i JOIN review_drafts d ON d.import_item_id=i.id
WHERE i.import_job_id=? AND i.state='REVIEW_PENDING'
ORDER BY i.id LIMIT ?`, importID, limit)
	if err != nil {
		return nil, fmt.Errorf("query batch reviews: %w", err)
	}
	defer func() { cleanup.Error("close batch reviews", rows.Close()) }()
	result := make([]application.ReviewBatchItem, 0)
	for rows.Next() {
		var item application.ReviewBatchItem
		if err := rows.Scan(&item.ItemID, &item.Version); err != nil {
			return nil, fmt.Errorf("scan batch review: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate batch reviews: %w", err)
	}
	return result, nil
}

func (repository *ReviewBatchDiscards) Release(ctx context.Context, importID string, now int64) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin discarded batch release: %w", err)
	}
	defer dbexec.Rollback(tx)
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM import_items WHERE import_job_id=?
AND state NOT IN ('PUBLISHED','DISCARDED','FAILED_FINAL','CANCELLED')`, importID).Scan(&pending); err != nil {
		return fmt.Errorf("check discarded batch: %w", err)
	}
	if pending != 0 {
		return application.ErrInvalid
	}
	if _, err := tx.ExecContext(ctx, `UPDATE import_jobs SET state='CANCELLED',cancel_reason='丢弃本批次未发布内容',
cancel_requested_at_ms=COALESCE(cancel_requested_at_ms,?),completed_at_ms=COALESCE(completed_at_ms,?),
updated_at_ms=?,version=version+1 WHERE id=? AND payload_state='RETAINED'`, now, now, now, importID); err != nil {
		return fmt.Errorf("close discarded batch: %w", err)
	}
	ids, err := payloadpersistence.CollectScopeIDs(ctx, tx, `
SELECT id FROM import_items WHERE import_job_id=? AND payload_state='RETAINED'`, importID)
	if err != nil {
		return fmt.Errorf("list discarded children: %w", err)
	}
	scheduler := payloadservice.NewScheduler(nil)
	for _, id := range ids {
		if _, err := scheduler.TerminalItem(ctx, payloadpersistence.BindScheduling(tx), id,
			payloadservice.ReasonImportDiscarded, now); err != nil {
			return fmt.Errorf("release discarded child: %w", err)
		}
	}
	if _, err := scheduler.TerminalImport(ctx, payloadpersistence.BindScheduling(tx), importID, now); err != nil {
		return fmt.Errorf("release discarded batch: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit discarded batch: %w", err)
	}
	return nil
}

var _ application.ReviewBatchDiscardRepository = (*ReviewBatchDiscards)(nil)
