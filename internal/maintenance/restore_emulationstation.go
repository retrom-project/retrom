package maintenance

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/recordstore"
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
	count, _ := jobs.RowsAffected()
	return count, nil
}

func clearRestoredEmulationStationScanStaging(ctx context.Context, transaction *sql.Tx) error {
	if _, err := recordstore.DeleteEmulationstationImportItemAssets(ctx, transaction, recordstore.Scope{
		Where: `
item_id IN (
 SELECT item.id FROM emulationstation_import_items item
 JOIN emulationstation_imports source ON source.id=item.import_id WHERE source.state='SCANNING')
`,
	}); err != nil {
		return fmt.Errorf("maintenance/bundle: clear restored EmulationStation staging: %w", err)
	}
	if _, err := recordstore.DeleteEmulationstationImportItemFiles(ctx, transaction, recordstore.Scope{
		Where: `
item_id IN (
 SELECT item.id FROM emulationstation_import_items item
 JOIN emulationstation_imports source ON source.id=item.import_id WHERE source.state='SCANNING')
`,
	}); err != nil {
		return fmt.Errorf("maintenance/bundle: clear restored EmulationStation staging: %w", err)
	}
	if _, err := recordstore.DeleteEmulationstationImportItems(ctx, transaction, recordstore.Scope{
		Where: `
import_id IN (
 SELECT id FROM emulationstation_imports WHERE state='SCANNING')
`,
	}); err != nil {
		return fmt.Errorf("maintenance/bundle: clear restored EmulationStation staging: %w", err)
	}
	if _, err := recordstore.DeleteEmulationstationCollectionTags(ctx, transaction, recordstore.Scope{
		Where: `
collection_id IN (
 SELECT collection.id FROM emulationstation_import_collections collection
 JOIN emulationstation_imports source ON source.id=collection.import_id WHERE source.state='SCANNING')
`,
	}); err != nil {
		return fmt.Errorf("maintenance/bundle: clear restored EmulationStation staging: %w", err)
	}
	if _, err := recordstore.DeleteEmulationstationImportCollections(ctx, transaction, recordstore.Scope{
		Where: `
import_id IN (
 SELECT id FROM emulationstation_imports WHERE state='SCANNING')
`,
	}); err != nil {
		return fmt.Errorf("maintenance/bundle: clear restored EmulationStation staging: %w", err)
	}
	if _, err := recordstore.DeleteEmulationstationImportGamelists(ctx, transaction, recordstore.Scope{
		Where: `
import_id IN (
 SELECT id FROM emulationstation_imports WHERE state='SCANNING')
`,
	}); err != nil {
		return fmt.Errorf("maintenance/bundle: clear restored EmulationStation staging: %w", err)
	}
	return nil
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

const restoredEmulationStationAggregateSQLScope = `state IN ('SCANNING','AWAITING_MAPPING','QUEUED','RUNNING','CANCEL_REQUESTED')`
