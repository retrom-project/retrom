package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/repo/recordstore"
	application "retrom/internal/service/libraryimport"
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
		return application.ErrInvalid
	}
	return nil
}

func (records reviewDiscardRecords) DiscardItem(ctx context.Context, change application.ReviewDiscardChange) error {
	transaction := records.executor
	itemID, importID, now := change.ItemID, change.ImportID, change.NowMS
	itemResult, itemErr := recordstore.UpdateImportItems(ctx, transaction, recordstore.Update{
		Set: `
state='DISCARDED',
version=version+1,
updated_at_ms=?,
completed_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `
id=?
AND state='REVIEW_PENDING'
AND EXISTS(SELECT 1 FROM review_drafts d WHERE d.import_item_id=import_items.id AND d.version=?)
`,
			Args: []any{itemID, change.ExpectedVersion},
		},
		Values: []any{now, now},
	})
	if err := requireDiscardMutation(itemResult, itemErr, "discard item"); err != nil {
		return err
	}
	jobResult, jobErr := transaction.ExecContext(ctx, `
UPDATE import_jobs
SET review_pending_item_count=review_pending_item_count-1,
discarded_item_count=discarded_item_count+1,
state=?,
version=version+1,
updated_at_ms=?,
completed_at_ms=?
WHERE id=? AND version=? AND review_pending_item_count=?
`, change.Aggregate.Projection.State, now, change.Aggregate.Projection.CompletedAtMS,
		importID, change.Aggregate.ExpectedVersion, change.Aggregate.ExpectedPending)
	return requireDiscardMutation(jobResult, jobErr, "discard job aggregate")
}
