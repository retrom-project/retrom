package recordstore

import (
	"context"
	"database/sql"
)

func UpdateImportItemDuplicateMatches(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"import_item_duplicate_matches",
		"import_item_id,existing_game_id",
		ImportItemDuplicateMatchesUpdateRule,
	)
}

const ImportItemDuplicateMatchesUpdateRule = `
WITH previous(import_item_id,existing_game_id) AS (VALUES(?,?))
SELECT CASE
-- import_item_duplicate_matches_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM import_item_duplicate_matches candidate CROSS JOIN previous
WHERE candidate.import_item_id=previous.import_item_id AND
candidate.existing_game_id=previous.existing_game_id`

func DeleteImportItemDuplicateMatches(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"import_item_duplicate_matches",
		"import_item_id,existing_game_id",
		ImportItemDuplicateMatchesDeleteRule,
	)
}

const ImportItemDuplicateMatchesDeleteRule = `
WITH previous(import_item_id,existing_game_id) AS (VALUES(?,?))
SELECT CASE
-- import_item_duplicate_matches_immutable_delete
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM previous`
