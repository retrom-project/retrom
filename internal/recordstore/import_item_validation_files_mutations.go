package recordstore

import (
	"context"
	"database/sql"
)

func UpdateImportItemValidationFiles(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"import_item_validation_files",
		"import_item_core_validation_id,role,logical_name",
		ImportItemValidationFilesUpdateRule,
	)
}

const ImportItemValidationFilesUpdateRule = `
WITH previous(import_item_core_validation_id,role,logical_name) AS (VALUES(?,?,?))
SELECT CASE
-- import_item_validation_files_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM import_item_validation_files candidate CROSS JOIN previous
WHERE candidate.import_item_core_validation_id=previous.import_item_core_validation_id AND
candidate.role=previous.role AND candidate.logical_name=previous.logical_name`

func DeleteImportItemValidationFiles(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"import_item_validation_files",
		"import_item_core_validation_id,role,logical_name",
		ImportItemValidationFilesDeleteRule,
	)
}

const ImportItemValidationFilesDeleteRule = `
WITH previous(import_item_core_validation_id,role,logical_name) AS (VALUES(?,?,?))
SELECT CASE
-- import_item_validation_files_immutable_delete
WHEN (NOT EXISTS(
  SELECT 1 FROM import_item_core_validations validation JOIN import_items item ON
item.id=validation.import_item_id
  WHERE validation.id=previous.import_item_core_validation_id AND item.payload_state IN ('RELEASING',
'FAILED')
)) THEN 'immutable'
ELSE '' END
FROM previous`
