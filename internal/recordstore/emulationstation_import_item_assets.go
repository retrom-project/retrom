package recordstore

import (
	"context"
	"database/sql"
)

func CreateEmulationstationImportItemAssets(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "item_id,kind", ValidateEmulationstationImportItemAssets)
}

func ValidateEmulationstationImportItemAssets(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, emulationstation_import_item_assetsOwnership, keys)
}

const emulationstation_import_item_assetsOwnership = `
SELECT CASE
WHEN (candidate.blob_id IS NOT NULL OR candidate.payload_released_at_ms IS NOT NULL OR NOT EXISTS(
  SELECT 1 FROM emulationstation_import_items item
  JOIN emulationstation_imports import ON import.id=item.import_id
  WHERE item.id=candidate.item_id AND import.state='SCANNING'
)) THEN 'invalid EmulationStation asset staging insert'
ELSE '' END
FROM emulationstation_import_item_assets candidate
WHERE candidate.item_id=? AND candidate.kind=?`
