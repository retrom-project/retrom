package importdiscard

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/payloadrelease"
)

// A crash can leave the internal upload envelope before an ImportJob is created.
// Only the stopped source batch's unconsumed envelopes are removable here.
func (service *Service) releaseUnusedUploads(ctx context.Context, kind, id string) error {
	table, err := batchTable(kind)
	if err != nil {
		return err
	}
	tx, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("importdiscard/unused envelopes: %w", err)
	}
	defer cleanup.Rollback(tx)
	ids, err := payloadrelease.CollectScopeIDs(ctx, tx, `
SELECT owner.upload_session_id FROM server_import_upload_owners owner
JOIN `+table[:len(table)-1]+`_items item ON item.id=owner.source_item_id
WHERE owner.kind=? AND item.import_id=?
AND NOT EXISTS(SELECT 1 FROM import_jobs WHERE upload_session_id=owner.upload_session_id)
AND NOT EXISTS(SELECT 1 FROM upload_consumptions WHERE upload_session_id=owner.upload_session_id)`, kind, id)
	if err != nil {
		return fmt.Errorf("importdiscard/list unused envelopes: %w", err)
	}
	for _, uploadID := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM upload_files WHERE upload_session_id=?`, uploadID); err != nil {
			return fmt.Errorf("importdiscard/remove unused upload files: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM upload_sessions WHERE id=?`, uploadID); err != nil {
			return fmt.Errorf("importdiscard/remove unused envelope: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("importdiscard/commit unused envelope release: %w", err)
	}
	return nil
}
