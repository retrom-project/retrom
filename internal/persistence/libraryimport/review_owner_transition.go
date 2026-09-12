package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/dbexec"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/libraryimport"
)

func transitionServerReviewOwner(
	ctx context.Context, transaction dbexec.Executor, table string, change application.ReviewOwnerTransition,
) (int64, error) {
	importItemID, state, gameID, now := change.ItemID, string(change.State), change.GameID, change.NowMS

	update := recordstore.UpdatePegasusImportItems
	switch table {
	case "pegasus_import_items":
	case "emulationstation_import_items":
		update = recordstore.UpdateEmulationstationImportItems
	default:
		return 0, application.ErrInvalid
	}
	result, err := update(ctx, transaction, recordstore.Update{
		Set: "execution_state=?,published_game_id=?,version=version+1,updated_at_ms=?", Values: []any{state, gameID, now},
		Scope: recordstore.Scope{Where: `library_import_item_id=? AND (execution_state='REVIEW_PENDING'
 OR ? AND ?='REVIEW_DISCARDED' AND execution_state NOT IN ('PUBLISHED','SKIPPED_EXISTING','REVIEW_DISCARDED'))
`, Args: []any{importItemID, change.Mode == application.ReviewDiscardBatch, state}},
	})
	if err != nil {
		return 0, fmt.Errorf("libraryimport/server review transition: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("libraryimport/server review rows affected: %w", err)
	}
	if affected == 0 {
		var linked int
		if err := transaction.QueryRowContext(
			ctx, `SELECT count(*) FROM `+table+` WHERE library_import_item_id=?`, importItemID,
		).Scan(&linked); err != nil {
			return 0, fmt.Errorf("libraryimport/server review link: %w", err)
		}
		if linked > 0 {
			return 0, application.ErrInvalid
		}
		return 0, nil
	}
	return affected, nil
}

func refreshPegasusReviewCounts(
	ctx context.Context, transaction dbexec.Executor, importItemID string, now int64,
) error {
	if _, err := recordstore.UpdatePegasusImports(ctx, transaction, recordstore.Update{
		Set: `
review_pending_item_count=(
  SELECT count(*) FROM pegasus_import_items item
  WHERE item.import_id=pegasus_imports.id AND item.execution_state='REVIEW_PENDING'
),
published_item_count=(
  SELECT count(*) FROM pegasus_import_items item
  WHERE item.import_id=pegasus_imports.id AND item.execution_state='PUBLISHED'
),
review_discarded_item_count=(
  SELECT count(*) FROM pegasus_import_items item
  WHERE item.import_id=pegasus_imports.id AND item.execution_state='REVIEW_DISCARDED'
),
version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `
id=(SELECT import_id FROM pegasus_import_items WHERE library_import_item_id=? LIMIT 1)
`,
			Args: []any{importItemID},
		},
		Values: []any{now},
	}); err != nil {
		return fmt.Errorf("libraryimport/server review aggregate: %w", err)
	}
	return nil
}

func refreshEmulationStationReviewCounts(
	ctx context.Context, transaction dbexec.Executor, importItemID string, now int64,
) error {
	if _, err := recordstore.UpdateEmulationstationImports(ctx, transaction, recordstore.Update{
		Set: `
review_pending_item_count=(
 SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state='REVIEW_PENDING'
),published_item_count=(
 SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state='PUBLISHED'
),review_discarded_item_count=(
 SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state='REVIEW_DISCARDED'
),version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `
id=(SELECT import_id FROM emulationstation_import_items
 WHERE library_import_item_id=? LIMIT 1)
`,
			Args: []any{importItemID},
		},
		Values: []any{now},
	}); err != nil {
		return fmt.Errorf("libraryimport/EmulationStation review aggregate: %w", err)
	}
	return nil
}
