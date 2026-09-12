package recordstore

import (
	"context"
	"database/sql"
)

func CreateEmulationstationImportItemFiles(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "item_id,ordinal", ValidateEmulationstationImportItemFiles)
}

func ValidateEmulationstationImportItemFiles(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, emulationstation_import_item_filesOwnership, keys)
}

const emulationstation_import_item_filesOwnership = `
SELECT CASE
WHEN (candidate.state<>'DISCOVERED' OR candidate.blob_id IS NOT NULL OR candidate.source_archive_blob_id
IS NOT NULL
  OR candidate.source_archive_entry_ordinal IS NOT NULL OR candidate.payload_released_at_ms IS NOT NULL
OR NOT EXISTS(
    SELECT 1 FROM emulationstation_import_items item
    JOIN emulationstation_imports import ON import.id=item.import_id
    WHERE item.id=candidate.item_id AND import.state='SCANNING'
  )) THEN 'invalid EmulationStation file staging insert'
ELSE '' END
FROM emulationstation_import_item_files candidate
WHERE candidate.item_id=? AND candidate.ordinal=?`
