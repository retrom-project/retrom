package recordstore

import (
	"context"
	"database/sql"
)

func UpdateImportItemSourceSnapshotFiles(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"import_item_source_snapshot_files",
		"source_snapshot_id,role,logical_name",
		ImportItemSourceSnapshotFilesUpdateRule,
	)
}

const ImportItemSourceSnapshotFilesUpdateRule = `
WITH previous(source_snapshot_id,role,logical_name) AS (VALUES(?,?,?))
SELECT CASE
-- import_item_source_snapshot_files_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM import_item_source_snapshot_files candidate CROSS JOIN previous
WHERE candidate.source_snapshot_id=previous.source_snapshot_id AND candidate.role=previous.role AND
candidate.logical_name=previous.logical_name`

func DeleteImportItemSourceSnapshotFiles(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"import_item_source_snapshot_files",
		"source_snapshot_id,role,logical_name",
		ImportItemSourceSnapshotFilesDeleteRule,
	)
}

const ImportItemSourceSnapshotFilesDeleteRule = `
WITH previous(source_snapshot_id,role,logical_name) AS (VALUES(?,?,?))
SELECT CASE
-- import_item_source_snapshot_files_immutable_delete
WHEN (NOT EXISTS(
  SELECT 1 FROM import_item_source_snapshots snapshot JOIN import_items item ON
item.id=snapshot.import_item_id
  WHERE snapshot.id=previous.source_snapshot_id AND item.payload_state IN ('RELEASING','FAILED')
)) THEN 'immutable'
ELSE '' END
FROM previous`
