package emulationstationimport

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/dbexec"
)

// ClearUnpublishedScan is the shared restoration and worker cleanup boundary.
// It refuses projections that already own mappings, copied content or review/game results.
func ClearUnpublishedScan(ctx context.Context, executor dbexec.Executor, importID string) error {
	return clearUnpublishedScan(ctx, executor, importID)
}

func clearUnpublishedScan(ctx context.Context, executor dbexec.Executor, importID string) error {
	var eligible bool
	err := executor.QueryRowContext(ctx, `SELECT import_job_id IS NULL AND scan_completed_at_ms IS NULL
AND source_snapshot_digest IS NULL AND state IN ('SCANNING','CANCEL_REQUESTED')
AND NOT EXISTS(SELECT 1 FROM emulationstation_import_items i WHERE i.import_id=plan.id
AND (i.execution_state<>'PENDING' OR i.library_import_job_id IS NOT NULL OR i.library_import_item_id IS NOT NULL
OR i.published_game_id IS NOT NULL OR i.existing_game_id IS NOT NULL OR i.payload_state<>'RETAINED'))
AND NOT EXISTS(SELECT 1 FROM emulationstation_import_item_files f JOIN emulationstation_import_items i ON i.id=f.item_id
WHERE i.import_id=plan.id AND (f.blob_id IS NOT NULL OR f.source_archive_blob_id IS NOT NULL OR f.state<>'DISCOVERED'))
AND NOT EXISTS(SELECT 1 FROM emulationstation_import_item_assets a
JOIN emulationstation_import_items i ON i.id=a.item_id
WHERE i.import_id=plan.id AND (a.blob_id IS NOT NULL OR a.state IN ('COPIED','PAYLOAD_RELEASED')))
AND NOT EXISTS(SELECT 1 FROM emulationstation_collection_tags tag
JOIN emulationstation_import_collections c ON c.id=tag.collection_id WHERE c.import_id=plan.id)
AND NOT EXISTS(SELECT 1 FROM emulationstation_import_collections c WHERE c.import_id=plan.id
AND (c.mapping_action IS NOT NULL OR c.tag_snapshot_json<>'[]'))
FROM emulationstation_imports plan WHERE id=?`, importID).Scan(&eligible)
	if err != nil {
		return fmt.Errorf("read unpublished EmulationStation scan ownership: %w", err)
	}
	if !eligible {
		return application.ErrVersionConflict
	}
	for _, target := range []struct{ table, where string }{
		{
			"emulationstation_import_item_assets",
			"item_id IN (SELECT id FROM emulationstation_import_items WHERE import_id=?)",
		},
		{"emulationstation_import_item_files", "item_id IN (SELECT id FROM emulationstation_import_items WHERE import_id=?)"},
		{"emulationstation_import_items", "import_id=?"},
		{"emulationstation_import_collections", "import_id=?"},
		{"emulationstation_import_gamelists", "import_id=?"},
	} {
		var count int64
		if err := executor.QueryRowContext(ctx, "SELECT count(*) FROM "+target.table+" WHERE "+target.where, importID).Scan(
			&count,
		); err != nil {
			return fmt.Errorf("count unpublished EmulationStation projection: %w", err)
		}
		result, err := executor.ExecContext(ctx, "DELETE FROM "+target.table+" WHERE "+target.where, importID)
		if err := requireRecoveryCount(result, err, count); err != nil {
			return err
		}
	}
	return nil
}

func requireRecoveryCount(result sql.Result, err error, want int64) error {
	if err != nil {
		return fmt.Errorf("change EmulationStation recovery projection: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count changed EmulationStation recovery projection: %w", err)
	}
	if count != want {
		return application.ErrVersionConflict
	}
	return nil
}
