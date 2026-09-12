package recordstore

import (
	"context"
	"database/sql"
)

func UpdateImportItemCoreValidations(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"import_item_core_validations",
		"id",
		ImportItemCoreValidationsUpdateRule,
	)
}

const ImportItemCoreValidationsUpdateRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- import_item_core_validations_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM import_item_core_validations candidate CROSS JOIN previous
WHERE candidate.id=previous.id`

func DeleteImportItemCoreValidations(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"import_item_core_validations",
		"id",
		ImportItemCoreValidationsDeleteRule,
	)
}

const ImportItemCoreValidationsDeleteRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- import_item_core_validations_immutable_delete
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM previous`
