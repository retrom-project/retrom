package recordstore

import (
	"context"
	"database/sql"
)

func UpdateImportItemSourceFiles(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"import_item_source_files",
		"import_item_id,role,logical_name",
		ImportItemSourceFilesUpdateRule,
	)
}

const ImportItemSourceFilesUpdateRule = `
WITH previous(import_item_id,role,logical_name) AS (VALUES(?,?,?))
SELECT CASE
-- import_item_source_files_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM import_item_source_files candidate CROSS JOIN previous
WHERE candidate.import_item_id=previous.import_item_id AND candidate.role=previous.role AND
candidate.logical_name=previous.logical_name`

func DeleteImportItemSourceFiles(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"import_item_source_files",
		"import_item_id,role,logical_name",
		ImportItemSourceFilesDeleteRule,
	)
}

const ImportItemSourceFilesDeleteRule = `
WITH previous(import_item_id,role,logical_name) AS (VALUES(?,?,?))
SELECT CASE
-- import_item_source_files_immutable_delete
WHEN (NOT EXISTS(SELECT 1 FROM import_items WHERE id=previous.import_item_id AND payload_state IN
('RELEASING','FAILED'))) THEN 'immutable'
ELSE '' END
FROM previous`
