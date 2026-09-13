package maintenance

import (
	"context"
	"fmt"

	"retrom/internal/repo/recordstore"
	"retrom/internal/service/maintenance"
)

func (writes writes) StopExternalImports(ctx context.Context, nowMS int64) (maintenance.ImportCounts, error) {
	transaction := writes.transaction
	serverJobs, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code='SERVER_IMPORT_SOURCE_NOT_RESTORED',error_retryable=0,
finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,
cancel_requested_at_ms=NULL,cancel_reason=NULL,version=version+1,updated_at_ms=?
WHERE kind='SERVER_BIOS_IMPORT' AND state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')
`, nowMS, nowMS)
	if err != nil {
		return maintenance.ImportCounts{}, fmt.Errorf("maintenance/bundle: fence restored server import jobs: %w", err)
	}
	pegasusJobs, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code='SERVER_IMPORT_SOURCE_NOT_RESTORED',error_retryable=0,
finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,
cancel_requested_at_ms=NULL,cancel_reason=NULL,version=version+1,updated_at_ms=?
WHERE kind IN ('SERVER_PEGASUS_SCAN','SERVER_PEGASUS_IMPORT') AND state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')
`, nowMS, nowMS)
	if err != nil {
		return maintenance.ImportCounts{}, fmt.Errorf("maintenance/bundle: fence restored Pegasus jobs: %w", err)
	}
	if err := clearRestoredPegasusScans(ctx, transaction); err != nil {
		return maintenance.ImportCounts{}, err
	}
	emulationStationJobCount, err := fenceRestoredEmulationStation(ctx, transaction, nowMS)
	if err != nil {
		return maintenance.ImportCounts{}, err
	}
	if _, err := recordstore.UpdateServerBiosImportItems(ctx, transaction, recordstore.Update{
		Set: `
state='COMMIT_FAILED',outcome_code='SERVER_IMPORT_SOURCE_NOT_RESTORED',
completed_at_ms=?,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `
server_import_id IN (
  SELECT id FROM server_imports WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')
) AND state IN ('PENDING','EVALUATING')
`,
		},
		Values: []any{nowMS, nowMS},
	}); err != nil {
		return maintenance.ImportCounts{}, fmt.Errorf("maintenance/bundle: fence restored server import items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE server_imports SET state='FAILED',last_error_code='SERVER_IMPORT_SOURCE_NOT_RESTORED',
cancel_requested_at_ms=NULL,cancel_reason=NULL,
imported_matched_count=(SELECT count(*) FROM server_bios_import_items item WHERE item.server_import_id=server_imports.id AND item.state='IMPORTED_MATCHED'),
imported_warning_count=(SELECT count(*) FROM server_bios_import_items item WHERE item.server_import_id=server_imports.id AND item.state='IMPORTED_WARNING'),
imported_missing_entry_count=(SELECT count(*) FROM server_bios_import_items item WHERE item.server_import_id=server_imports.id AND item.state='IMPORTED_MISSING_ENTRY'),
not_found_count=(SELECT count(*) FROM server_bios_import_items item
  WHERE item.server_import_id=server_imports.id AND item.state='NOT_FOUND'),
skipped_existing_count=(SELECT count(*) FROM server_bios_import_items item
  WHERE item.server_import_id=server_imports.id AND item.state='SKIPPED_EXISTING'),
skipped_not_better_count=(SELECT count(*) FROM server_bios_import_items item
  WHERE item.server_import_id=server_imports.id AND item.state='SKIPPED_NOT_BETTER'),
same_bytes_count=(SELECT count(*) FROM server_bios_import_items item
  WHERE item.server_import_id=server_imports.id AND item.state='ALREADY_SAME_BYTES'),
failed_item_count=(SELECT count(*) FROM server_bios_import_items item
  WHERE item.server_import_id=server_imports.id
  AND item.state IN ('SOURCE_CHANGED','CATALOG_CHANGED','READ_FAILED','COMMIT_FAILED')),
cancelled_item_count=(SELECT count(*) FROM server_bios_import_items item
  WHERE item.server_import_id=server_imports.id AND item.state='CANCELLED'),
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')
`, nowMS, nowMS); err != nil {
		return maintenance.ImportCounts{}, fmt.Errorf("maintenance/bundle: fence restored server imports: %w", err)
	}
	if _, err := recordstore.UpdatePegasusImportItems(ctx, transaction, recordstore.Update{
		Set: `
execution_state='COMMIT_FAILED',error_code='SERVER_IMPORT_SOURCE_NOT_RESTORED',
retryable=0,completed_at_ms=?,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `
import_id IN (
  SELECT id FROM pegasus_imports WHERE state IN ('SCANNING','AWAITING_MAPPING','QUEUED','RUNNING',
'CANCEL_REQUESTED')
) AND execution_state IN ('PENDING','COPYING','VALIDATING')
`,
		},
		Values: []any{nowMS, nowMS},
	}); err != nil {
		return maintenance.ImportCounts{}, fmt.Errorf("maintenance/bundle: fence restored Pegasus items: %w", err)
	}
	if _, err := recordstore.UpdatePegasusImports(ctx, transaction, recordstore.Update{
		Set: `
state='FAILED',phase=NULL,last_error_code='SERVER_IMPORT_SOURCE_NOT_RESTORED',
retryable=0,cancel_reason=NULL,
review_pending_item_count=(SELECT count(*) FROM pegasus_import_items item
  WHERE item.import_id=pegasus_imports.id AND item.execution_state='REVIEW_PENDING'),
review_discarded_item_count=(SELECT count(*) FROM pegasus_import_items item
  WHERE item.import_id=pegasus_imports.id AND item.execution_state='REVIEW_DISCARDED'),
published_item_count=(SELECT count(*) FROM pegasus_import_items item
  WHERE item.import_id=pegasus_imports.id AND item.execution_state='PUBLISHED'),
existing_item_count=(SELECT count(*) FROM pegasus_import_items item
  WHERE item.import_id=pegasus_imports.id AND item.execution_state='SKIPPED_EXISTING'),
blocked_item_count=(SELECT count(*) FROM pegasus_import_items item
  WHERE item.import_id=pegasus_imports.id
  AND item.execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT')),
failed_item_count=(SELECT count(*) FROM pegasus_import_items item
  WHERE item.import_id=pegasus_imports.id
  AND item.execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')),
cancelled_item_count=(SELECT count(*) FROM pegasus_import_items item
  WHERE item.import_id=pegasus_imports.id AND item.execution_state='CANCELLED'),
completed_at_ms=?,version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `state IN ('SCANNING','AWAITING_MAPPING','QUEUED','RUNNING','CANCEL_REQUESTED')`,
		},
		Values: []any{nowMS, nowMS},
	}); err != nil {
		return maintenance.ImportCounts{}, fmt.Errorf("maintenance/bundle: fence restored Pegasus imports: %w", err)
	}

	counts, err := affected(serverJobs, pegasusJobs)
	if err != nil {
		return maintenance.ImportCounts{}, err
	}
	return maintenance.ImportCounts{BIOS: counts[0], Pegasus: counts[1], EmulationStation: emulationStationJobCount}, nil
}
