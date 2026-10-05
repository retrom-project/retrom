package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/persistence/recordstore"
	libraryservice "retrom/internal/service/libraryimport"
)

func requireDiscardMutation(result sql.Result, err error, action string) error {
	if err != nil {
		return fmt.Errorf("libraryimport/review: %s: %w", action, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("libraryimport/review: %s result: %w", action, err)
	}
	if changed != 1 {
		return libraryservice.ErrInvalid
	}
	return nil
}

func (records reviewDiscardRecords) DiscardItem(ctx context.Context, change libraryservice.ReviewDiscardChange) error {
	transaction := records.executor
	itemID, importID, now := change.ItemID, change.ImportID, change.NowMS
	skipped := 0
	var bulkID *string
	if change.Existing != nil {
		skipped, bulkID = 1, change.Existing.BulkID
	}
	itemResult, itemErr := recordstore.UpdateImportItems(ctx, transaction, recordstore.Update{
		Set: `
state='DISCARDED',
version=version+1,
updated_at_ms=?,
completed_at_ms=?,publication_bulk_id=?
`,
		Scope: recordstore.Scope{
			Where: `
id=?
AND state='REVIEW_PENDING'
AND EXISTS(SELECT 1 FROM import_items d WHERE d.id=import_items.id AND d.review_version=?)
`,
			Args: []any{itemID, change.ExpectedVersion},
		},
		Values: []any{now, now, bulkID},
	})
	if err := requireDiscardMutation(itemResult, itemErr, "discard item"); err != nil {
		return err
	}
	jobResult, jobErr := transaction.ExecContext(ctx, `
UPDATE import_jobs
SET review_pending_item_count=review_pending_item_count-1,
discarded_item_count=discarded_item_count+1,
already_imported_item_count=already_imported_item_count+?,
already_imported_file_count=already_imported_file_count+?*(SELECT count(*) FROM import_item_source_snapshot_files
 WHERE source_snapshot_id=(SELECT effective_source_snapshot_id FROM import_items WHERE id=?)),
state=?,
version=version+1,
updated_at_ms=?,
completed_at_ms=?
WHERE id=? AND version=? AND review_pending_item_count=?
`, skipped, skipped, itemID, change.Aggregate.Projection.State, now, change.Aggregate.Projection.CompletedAtMS,
		importID, change.Aggregate.ExpectedVersion, change.Aggregate.ExpectedPending)
	if err := requireDiscardMutation(jobResult, jobErr, "discard job aggregate"); err != nil {
		return err
	}
	if change.Existing != nil {
		for _, game := range change.Existing.Games {
			if _, err := transaction.ExecContext(ctx, `INSERT INTO import_item_duplicate_matches
(import_item_id,existing_game_id,content_identity_digest,detected_stage,created_at_ms)
VALUES(?,?,?,'REVIEW',?)`, itemID, game.GameID, change.Existing.Identity, now); err != nil {
				return fmt.Errorf("record skipped duplicate: %w", err)
			}
		}
	}
	return closeReview(ctx, transaction, itemID, now)
}
