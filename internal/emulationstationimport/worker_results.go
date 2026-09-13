package emulationstationimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"retrom/internal/persistence/recordstore"

	"retrom/internal/libraryimport"
	application "retrom/internal/service/emulationstationimport"
)

func (service *Service) attachLibraryResult(
	ctx context.Context,
	itemID, importJobID string,
	imported libraryimport.ServerImportItem,
) error {
	now := service.now().UnixMilli()
	result, err := recordstore.UpdateEmulationstationImportItems(ctx, service.database, recordstore.Update{
		Set: `
execution_state='VALIDATING',library_import_job_id=?,library_import_item_id=?,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=? AND execution_state='COPYING'`,
			Args:  []any{itemID},
		},
		Values: []any{importJobID, imported.ItemID, now},
	})
	if err != nil {
		return fmt.Errorf("emulationstationimport/attach library result: %w", err)
	}
	if rowsAffected(result) != 1 {
		return fmt.Errorf("emulationstationimport/attach library result: %w", errItemStateChanged)
	}
	return nil
}

func (service *Service) closeItem(ctx context.Context, unit work, itemID, state, code string,
	retryable bool,
) error {
	return service.closeItemWithFailure(ctx, unit, itemID, state, code, retryable, nil)
}

func (service *Service) closeItemWithFailure(ctx context.Context, unit work, itemID, state, code string,
	retryable bool, failure *FailureDetails,
) error {
	return service.finishItemOutcome(ctx, unit, itemID, application.ItemOutcome{
		State: state, Code: code, Retryable: retryable, Failure: failure,
	})
}

func (service *Service) refreshCountsAndEvent(
	ctx context.Context,
	transaction *sql.Tx,
	unit work,
	itemID, outcome string,
	now int64,
) error {
	if _, err := recordstore.UpdateEmulationstationImports(ctx, transaction, recordstore.Update{
		Set: `
review_pending_item_count=(
  SELECT count(*) FROM emulationstation_import_items
  WHERE import_id=? AND execution_state='REVIEW_PENDING'
),
published_item_count=(
  SELECT count(*) FROM emulationstation_import_items
  WHERE import_id=? AND execution_state='PUBLISHED'
),
review_discarded_item_count=(
  SELECT count(*) FROM emulationstation_import_items
  WHERE import_id=? AND execution_state='REVIEW_DISCARDED'
),
existing_item_count=(
  SELECT count(*) FROM emulationstation_import_items
  WHERE import_id=? AND execution_state='SKIPPED_EXISTING'
),
blocked_item_count=(
  SELECT count(*) FROM emulationstation_import_items
  WHERE import_id=? AND execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT')
),
failed_item_count=(
  SELECT count(*) FROM emulationstation_import_items
  WHERE import_id=? AND execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')
),
cancelled_item_count=(
  SELECT count(*) FROM emulationstation_import_items
  WHERE import_id=? AND execution_state='CANCELLED'
),
media_warning_count=(
  SELECT count(*)
  FROM emulationstation_import_items item,json_each(item.warnings_json) warning
  WHERE item.import_id=?
  AND json_extract(warning.value,'$.pathKind') IN ('COVER','VIDEO')
),
version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=?`,
			Args:  []any{unit.ImportID},
		},
		Values: []any{
			unit.ImportID,
			unit.ImportID,
			unit.ImportID,
			unit.ImportID,
			unit.ImportID,
			unit.ImportID,
			unit.ImportID,
			unit.ImportID,
			now,
		},
	}); err != nil {
		return fmt.Errorf("emulationstationimport/refresh aggregate counts: %w", err)
	}
	data, _ := json.Marshal(map[string]any{"schemaVersion": 1, "itemId": itemID, "outcome": outcome})
	_, err := transaction.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'EMULATIONSTATION_IMPORT',?,'PROGRESS',?,?)`,
		unit.JobID,
		unit.ImportID,
		string(data),
		now,
	)
	if err != nil {
		return fmt.Errorf("emulationstationimport/create progress event: %w", err)
	}
	return nil
}

func (service *Service) closeCancelled(ctx context.Context, unit work) (bool, error) {
	closed, err := service.executionControl().CloseCancelled(ctx, unit)
	if err != nil {
		return false, fmt.Errorf("close cancelled EmulationStation execution: %w", err)
	}
	return closed, nil
}
