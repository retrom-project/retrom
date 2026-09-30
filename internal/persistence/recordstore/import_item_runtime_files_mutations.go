package recordstore

import (
	"context"
	"database/sql"

	dbapi "retrom/internal/database"
)

func UpdateImportItemRuntimeFiles(
	ctx context.Context, db dbapi.Executor, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"import_item_runtime_files",
		"import_item_id,role,logical_name",
		ImportItemRuntimeFilesUpdateRule,
	)
}

const ImportItemRuntimeFilesUpdateRule = `
WITH previous(import_item_id,role,logical_name) AS (VALUES(?,?,?))
SELECT CASE
-- import_item_runtime_files_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM import_item_runtime_files candidate CROSS JOIN previous
WHERE candidate.import_item_id=previous.import_item_id AND
candidate.role=previous.role AND candidate.logical_name=previous.logical_name`

func DeleteImportItemRuntimeFiles(
	ctx context.Context, db dbapi.Executor, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"import_item_runtime_files",
		"import_item_id,role,logical_name",
		ImportItemRuntimeFilesDeleteRule,
	)
}

const ImportItemRuntimeFilesDeleteRule = `
WITH previous(import_item_id,role,logical_name) AS (VALUES(?,?,?))
SELECT CASE
-- import_item_runtime_files_immutable_delete
WHEN (NOT EXISTS(
  SELECT 1 FROM import_items item WHERE item.id=previous.import_item_id
  AND item.payload_state='RELEASING'
)) THEN 'immutable'
ELSE '' END
FROM previous`
