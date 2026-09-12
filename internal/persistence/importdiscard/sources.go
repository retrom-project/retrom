package importdiscard

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/payloadrelease"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/service/importdiscard"
)

func (writes writes) Complete(ctx context.Context, key importdiscard.Key, now int64) error {
	kind, id := key.Kind, key.ID
	table, err := batchTable(kind)
	if err != nil {
		return err
	}
	itemsTable := table[:len(table)-1] + "_items"
	tx := writes.transaction
	skipped := ""
	if kind == "EMULATIONSTATION" {
		skipped = "skipped_mapping_item_count=0,"
	}
	updateItems := recordstore.UpdatePegasusImportItems
	updateBatch := recordstore.UpdatePegasusImports
	if kind == "EMULATIONSTATION" {
		updateItems = recordstore.UpdateEmulationstationImportItems
		updateBatch = recordstore.UpdateEmulationstationImports
	}
	if _, err := updateItems(ctx, tx, recordstore.Update{
		Set: `execution_state='REVIEW_DISCARDED',retryable=0,
 completed_at_ms=COALESCE(completed_at_ms,?),updated_at_ms=?,version=version+1`, Values: []any{now, now},
		Scope: recordstore.Scope{Where: `
import_id=? AND execution_state NOT IN ('PUBLISHED','SKIPPED_EXISTING','REVIEW_DISCARDED')
`, Args: []any{id}},
	}); err != nil {
		return fmt.Errorf("importdiscard/discard source items: %w", err)
	}
	ids, err := payloadrelease.CollectScopeIDs(ctx, tx, `
SELECT id FROM `+itemsTable+` WHERE import_id=? AND payload_state='RETAINED'`, id)
	if err != nil {
		return fmt.Errorf("importdiscard/list source releases: %w", err)
	}
	for _, item := range ids {
		if err := scheduleSourceRelease(ctx, tx, kind, item, now); err != nil {
			return err
		}
	}
	if _, err := updateBatch(ctx, tx, recordstore.Update{
		Set: skipped + `state='COMPLETED',phase=NULL,cancel_reason=NULL,retryable=0,
 completed_at_ms=COALESCE(completed_at_ms,?),updated_at_ms=?,version=version+1,
 review_pending_item_count=0,blocked_item_count=0,failed_item_count=0,cancelled_item_count=0,
 review_discarded_item_count=(SELECT count(*) FROM ` + itemsTable + `
 WHERE import_id=? AND execution_state='REVIEW_DISCARDED')`, Values: []any{now, now, id},
		Scope: recordstore.Scope{Where: "id=?", Args: []any{id}},
	}); err != nil {
		return fmt.Errorf("importdiscard/close source batch: %w", err)
	}
	return nil
}

func scheduleSourceRelease(ctx context.Context, tx *sql.Tx, kind, id string, now int64) error {
	var err error
	if kind == "PEGASUS" {
		_, err = payloadrelease.ScheduleTerminalPegasusItem(ctx, tx, id, now)
	} else {
		_, err = payloadrelease.ScheduleTerminalEmulationStationItem(ctx, tx, id, now)
	}
	if err != nil {
		return fmt.Errorf("importdiscard/schedule source release: %w", err)
	}
	return nil
}
