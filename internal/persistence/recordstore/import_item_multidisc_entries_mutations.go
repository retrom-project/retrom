package recordstore

import (
	"context"
	"database/sql"

	dbapi "retrom/internal/database"
)

func UpdateImportItemMultidiscEntries(
	ctx context.Context, db dbapi.Executor, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"import_item_multidisc_entries",
		"source_snapshot_id,ordinal,state",
		ImportItemMultidiscEntriesUpdateRule,
	)
}

const ImportItemMultidiscEntriesUpdateRule = `
WITH previous(source_snapshot_id,ordinal,state)
AS (VALUES(?::text,?::bigint,?::text))
SELECT CASE
-- import_item_multidisc_entries_immutable_update
WHEN (NOT (
  previous.state='PRESENT' AND candidate.state='RELEASED'
  AND candidate.upload_file_id IS NULL AND candidate.file_record IS NULL AND
candidate.payload_released_at_ms IS NOT NULL
  AND EXISTS(
    SELECT 1 FROM import_item_source_snapshots snapshot JOIN import_items item ON
item.id=snapshot.import_item_id
    WHERE snapshot.id=previous.source_snapshot_id AND item.payload_state ='RELEASING'
  )
)) THEN 'immutable'
ELSE '' END
FROM import_item_multidisc_entries candidate CROSS JOIN previous
WHERE candidate.source_snapshot_id=previous.source_snapshot_id AND candidate.ordinal=previous.ordinal`

func DeleteImportItemMultidiscEntries(
	ctx context.Context, db dbapi.Executor, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"import_item_multidisc_entries",
		"source_snapshot_id,ordinal",
		ImportItemMultidiscEntriesDeleteRule,
	)
}

const ImportItemMultidiscEntriesDeleteRule = `
WITH previous(source_snapshot_id,ordinal)
AS (VALUES(?::text,?::bigint))
SELECT CASE
-- import_item_multidisc_entries_immutable_delete
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM previous`
