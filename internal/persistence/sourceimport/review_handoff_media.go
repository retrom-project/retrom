package sourceimport

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"

	"retrom/internal/persistence/fileownership"
	"retrom/internal/persistence/recordstore"
)

// TransferReviewMedia gives the review item its own references before Source release is queued.
// The state transition, references, metadata and release job share the handoff transaction.
func TransferReviewMedia(ctx context.Context, tx dbapi.Executor, sourceID, itemID string, now int64) error {
	if err := fileownership.TransferSelected(ctx, tx,
		fileownership.Owner{Kind: "SOURCE_IMPORT_ITEM", ID: sourceID}, fileownership.Owner{Kind: "IMPORT_ITEM", ID: itemID},
		`SELECT blob_id FROM source_import_item_assets WHERE item_id=? AND state='COPIED' AND blob_id
IS NOT NULL`, sourceID); err != nil {
		return fmt.Errorf("review handoff media: %w", err)
	}
	_, err := recordstore.InsertRows(ctx, tx, "import_item_assets", `
 INSERT INTO import_item_assets(import_item_id,kind,blob_id,media_type,width_px,height_px,
created_at_ms)
 SELECT ?,kind,blob_id,media_type,width_px,height_px,? FROM source_import_item_assets
 WHERE item_id=? AND state='COPIED' AND blob_id IS NOT NULL`, itemID, now, sourceID)
	if err != nil {
		return fmt.Errorf("transfer Source media ownership: %w", err)
	}
	return nil
}
