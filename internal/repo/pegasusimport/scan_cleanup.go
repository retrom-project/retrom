package pegasusimport

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/pegasusimport"
)

// ClearUnpublishedScan is called after the enclosing transaction fences the scan job.
// Permanent review, payload and mapping ownership must never be erased by scan cleanup.
func ClearUnpublishedScan(ctx context.Context, tx *sql.Tx, importID string) error {
	var eligible bool
	err := tx.QueryRowContext(ctx, `SELECT import_job_id IS NULL AND scan_completed_at_ms IS NULL
AND state IN ('SCANNING','CANCEL_REQUESTED')
AND NOT EXISTS(SELECT 1 FROM pegasus_import_items i WHERE i.import_id=plan.id
 AND (i.library_import_item_id IS NOT NULL OR i.library_import_job_id IS NOT NULL OR i.execution_state<>'PENDING'))
AND NOT EXISTS(SELECT 1 FROM pegasus_import_item_files f JOIN pegasus_import_items i ON i.id=f.item_id
 WHERE i.import_id=plan.id AND (f.blob_id IS NOT NULL OR f.source_archive_blob_id IS NOT NULL OR f.state<>'DISCOVERED'))
AND NOT EXISTS(SELECT 1 FROM pegasus_import_item_assets a JOIN pegasus_import_items i ON i.id=a.item_id
 WHERE i.import_id=plan.id AND (a.blob_id IS NOT NULL OR a.state<>'DISCOVERED'))
AND NOT EXISTS(SELECT 1 FROM pegasus_collection_tags tag
 JOIN pegasus_import_collections c ON c.id=tag.collection_id WHERE c.import_id=plan.id)
FROM pegasus_imports plan WHERE id=?`, importID).Scan(&eligible)
	if err != nil {
		return fmt.Errorf("read unpublished Pegasus scan ownership: %w", err)
	}
	if !eligible {
		return application.ErrVersionConflict
	}
	for _, query := range []string{
		`DELETE FROM pegasus_import_item_assets WHERE item_id IN(SELECT id FROM pegasus_import_items WHERE import_id=?)`,
		`DELETE FROM pegasus_import_item_files WHERE item_id IN(SELECT id FROM pegasus_import_items WHERE import_id=?)`,
		`DELETE FROM pegasus_import_items WHERE import_id=?`,
		`DELETE FROM pegasus_import_collections WHERE import_id=?`,
		`DELETE FROM pegasus_import_metadata_files WHERE import_id=?`,
	} {
		if _, err := tx.ExecContext(ctx, query, importID); err != nil {
			return fmt.Errorf("clear unpublished Pegasus scan: %w", err)
		}
	}
	return nil
}
