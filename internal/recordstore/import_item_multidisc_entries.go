package recordstore

import (
	"context"
	"database/sql"
)

func CreateImportItemMultidiscEntries(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "source_snapshot_id,ordinal", ValidateImportItemMultidiscEntries)
}

func ValidateImportItemMultidiscEntries(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, import_item_multidisc_entriesOwnership, keys)
}

const import_item_multidisc_entriesOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM import_item_source_snapshots snapshot
  WHERE snapshot.id=candidate.source_snapshot_id AND snapshot.content_kind='MULTI_DISC'
)
OR candidate.state='PRESENT' AND NOT EXISTS(
  SELECT 1 FROM import_item_source_snapshot_files file
  WHERE file.source_snapshot_id=candidate.source_snapshot_id AND file.role='DISC'
  AND file.upload_file_id=candidate.upload_file_id AND file.blob_id=candidate.blob_id
  AND file.logical_name=candidate.source_logical_name AND file.sort_order=candidate.ordinal
)) THEN 'invalid multi-disc entry owner'
ELSE '' END
FROM import_item_multidisc_entries candidate
WHERE candidate.source_snapshot_id=? AND candidate.ordinal=?`
