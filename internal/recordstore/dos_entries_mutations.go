package recordstore

import (
	"context"
	"database/sql"
)

func UpdateDosEntries(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"dos_entries",
		"game_id,normalized_path",
		DosEntriesUpdateRule,
	)
}

const DosEntriesUpdateRule = `
WITH previous(game_id,normalized_path) AS (VALUES(?,?))
SELECT CASE
-- dos_entries_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM dos_entries candidate CROSS JOIN previous
WHERE candidate.game_id=previous.game_id AND candidate.normalized_path=previous.normalized_path`

func DeleteDosEntries(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"dos_entries",
		"game_id,normalized_path",
		DosEntriesDeleteRule,
	)
}

const DosEntriesDeleteRule = `
WITH previous(game_id,normalized_path) AS (VALUES(?,?))
SELECT CASE
-- dos_entries_immutable_delete
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM previous`
