package maintenance

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/foundation/cleanup"
	esrepository "retrom/internal/persistence/emulationstationimport"
	"retrom/internal/persistence/recordstore"
)

func fenceRestoredEmulationStation(
	ctx context.Context,
	transaction *sql.Tx,
	nowMS int64,
) (int64, error) {
	jobs, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code='SERVER_IMPORT_SOURCE_NOT_RESTORED',error_retryable=0,
finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,
cancel_requested_at_ms=NULL,cancel_reason=NULL,version=version+1,updated_at_ms=?
WHERE kind IN ('SERVER_EMULATIONSTATION_SCAN','SERVER_EMULATIONSTATION_IMPORT')
AND state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')
`, nowMS, nowMS)
	if err != nil {
		return 0, fmt.Errorf("maintenance/bundle: fence restored EmulationStation jobs: %w", err)
	}
	if err := clearRestoredEmulationStationScanStaging(ctx, transaction); err != nil {
		return 0, err
	}
	if _, err := recordstore.UpdateEmulationstationImportItems(ctx, transaction, recordstore.Update{
		Set: `
execution_state='COMMIT_FAILED',error_code='SERVER_IMPORT_SOURCE_NOT_RESTORED',
retryable=0,completed_at_ms=?,version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `
import_id IN (
 SELECT id FROM emulationstation_imports
 WHERE state IN ('AWAITING_MAPPING','QUEUED','RUNNING','CANCEL_REQUESTED')
)
AND execution_state IN ('PENDING','COPYING','VALIDATING')
`,
		},
		Values: []any{nowMS, nowMS},
	}); err != nil {
		return 0, fmt.Errorf("maintenance/bundle: fence restored EmulationStation items: %w", err)
	}
	if _, err := recordstore.UpdateEmulationstationImports(ctx, transaction, recordstore.Update{
		Set: restoredEmulationStationAggregateSQLAssignments,
		Scope: recordstore.Scope{
			Where: restoredEmulationStationAggregateSQLScope,
		},
		Values: []any{nowMS, nowMS},
	}); err != nil {
		return 0, fmt.Errorf("maintenance/bundle: fence restored EmulationStation imports: %w", err)
	}
	count, err := jobs.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count restored source jobs: %w", err)
	}
	return count, nil
}

func clearRestoredEmulationStationScanStaging(ctx context.Context, transaction *sql.Tx) error {
	ids, err := restoredEmulationStationScans(ctx, transaction)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := esrepository.ClearUnpublishedScan(ctx, transaction, id); err != nil {
			return fmt.Errorf("clear restored EmulationStation scan: %w", err)
		}
		result, err := transaction.ExecContext(ctx, `UPDATE emulationstation_imports SET
  gamelist_count=0,invalid_gamelist_count=0,collection_count=0,folder_entry_count=0,game_count=0,
  estimated_source_bytes=0,mapped_collection_count=0,skipped_collection_count=0,processable_item_count=0,
  blocked_item_count=0,media_warning_count=0,discovered_cover_count=0,discovered_video_count=0
  WHERE id=? AND import_job_id IS NULL AND state IN ('SCANNING','CANCEL_REQUESTED')`, id)
		if err != nil {
			return fmt.Errorf("clear restored EmulationStation scan counters: %w", err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("count cleared restored EmulationStation scans: %w", err)
		}
		if count != 1 {
			return fmt.Errorf("clear restored EmulationStation scan %s: %w", id, sql.ErrNoRows)
		}
	}
	return nil
}

func restoredEmulationStationScans(ctx context.Context, transaction *sql.Tx) ([]string, error) {
	rows, err := transaction.QueryContext(ctx, `SELECT id FROM emulationstation_imports
 WHERE import_job_id IS NULL AND state IN ('SCANNING','CANCEL_REQUESTED') ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list restored EmulationStation scans: %w", err)
	}
	defer func() { cleanup.Error("close restored EmulationStation scans", rows.Close()) }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("read restored EmulationStation scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate restored EmulationStation scans: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close restored EmulationStation scans: %w", err)
	}
	return ids, nil
}

const restoredEmulationStationAggregateSQLAssignments = `state='FAILED',phase=NULL,
last_error_code='SERVER_IMPORT_SOURCE_NOT_RESTORED',retryable=0,cancel_reason=NULL,
skipped_mapping_item_count=(SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state='SKIPPED_MAPPING'),
review_pending_item_count=(SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state='REVIEW_PENDING'),
published_item_count=(SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state='PUBLISHED'),
review_discarded_item_count=(SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state='REVIEW_DISCARDED'),
existing_item_count=(SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state='SKIPPED_EXISTING'),
blocked_item_count=(SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id
 AND item.execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT')),
failed_item_count=(SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id
 AND item.execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')),
cancelled_item_count=(SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state='CANCELLED'),
completed_at_ms=?,version=version+1,updated_at_ms=?`

const restoredEmulationStationAggregateSQLScope = `
state IN ('SCANNING','AWAITING_MAPPING','QUEUED','RUNNING','CANCEL_REQUESTED')`
