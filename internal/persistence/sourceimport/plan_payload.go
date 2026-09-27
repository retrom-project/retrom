package sourceimport

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
)

func clearPlanPayload(ctx context.Context, tx dbapi.Executor, importID string) error {
	for _, table := range []string{
		"source_import_item_companions", "source_import_item_assets", "source_import_item_files",
	} {
		if _, err := recordstore.DeleteReferences(ctx, tx, table, recordstore.Scope{
			Where: `item_id IN (SELECT id FROM source_import_items WHERE import_id=?)`, Args: []any{importID},
		}); err != nil {
			return fmt.Errorf("clear unpublished Source payload: %w", err)
		}
	}
	return nil
}
