package sourceimport

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"

	"retrom/internal/persistence/recordstore"
)

// TransferReviewMedia gives the review item its own references before Source release is queued.
// The state transition, references, metadata and release job share the handoff transaction.
func TransferReviewMedia(ctx context.Context, tx dbapi.Executor, sourceID, itemID string, now int64) error {
	_, err := recordstore.CreateReferences(ctx, tx, "import_item_assets", `
 INSERT INTO import_item_assets(import_item_id,kind,blob_id,media_type,width_px,height_px,created_at_ms)
 SELECT ?,kind,blob_id,media_type,width_px,height_px,? FROM source_import_item_assets
 WHERE item_id=? AND state='COPIED' AND blob_id IS NOT NULL`, itemID, now, sourceID)
	if err != nil {
		return fmt.Errorf("transfer Source media ownership: %w", err)
	}
	return nil
}
