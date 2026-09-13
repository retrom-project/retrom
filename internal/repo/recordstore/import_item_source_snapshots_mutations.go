package recordstore

import (
	"context"
	"database/sql"

	"retrom/internal/repo/dbexec"
)

func UpdateImportItemSourceSnapshots(
	ctx context.Context, db dbexec.Executor, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"import_item_source_snapshots",
		"id",
		ImportItemSourceSnapshotsUpdateRule,
	)
}

const ImportItemSourceSnapshotsUpdateRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- import_item_source_snapshots_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM import_item_source_snapshots candidate CROSS JOIN previous
WHERE candidate.id=previous.id`

func DeleteImportItemSourceSnapshots(
	ctx context.Context, db dbexec.Executor, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"import_item_source_snapshots",
		"id",
		ImportItemSourceSnapshotsDeleteRule,
	)
}

const ImportItemSourceSnapshotsDeleteRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- import_item_source_snapshots_immutable_delete
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM previous`
