package recordstore

import (
	"context"
	"database/sql"
)

func UpdateEmulationstationImportGamelists(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"emulationstation_import_gamelists",
		"import_id,relative_path",
		EmulationstationImportGamelistsUpdateRule,
	)
}

const EmulationstationImportGamelistsUpdateRule = `
WITH previous(import_id,relative_path) AS (VALUES(?,?))
SELECT CASE
-- emulationstation_gamelists_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM emulationstation_import_gamelists candidate CROSS JOIN previous
WHERE candidate.import_id=previous.import_id AND candidate.relative_path=previous.relative_path`

func DeleteEmulationstationImportGamelists(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"emulationstation_import_gamelists",
		"import_id,relative_path",
		EmulationstationImportGamelistsDeleteRule,
	)
}

const EmulationstationImportGamelistsDeleteRule = `
WITH previous(import_id,relative_path) AS (VALUES(?,?))
SELECT CASE
-- emulationstation_gamelist_delete
WHEN (NOT EXISTS(SELECT 1 FROM emulationstation_imports import
  WHERE import.id=previous.import_id AND import.state IN ('SCANNING','AWAITING_MAPPING','EXPIRED')))
THEN 'EmulationStation gamelist snapshot is frozen'
ELSE '' END
FROM previous`
