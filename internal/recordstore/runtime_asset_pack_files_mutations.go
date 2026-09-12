package recordstore

import (
	"context"
	"database/sql"
)

func UpdateRuntimeAssetPackFiles(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"runtime_asset_pack_files",
		"installation_id,path",
		RuntimeAssetPackFilesUpdateRule,
	)
}

const RuntimeAssetPackFilesUpdateRule = `
WITH previous(installation_id,path) AS (VALUES(?,?))
SELECT CASE
-- runtime_asset_pack_files_immutable_update
WHEN (1=1) THEN 'runtime pack file is immutable'
ELSE '' END
FROM runtime_asset_pack_files candidate CROSS JOIN previous
WHERE candidate.installation_id=previous.installation_id AND candidate.path=previous.path`

func DeleteRuntimeAssetPackFiles(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"runtime_asset_pack_files",
		"installation_id,path",
		RuntimeAssetPackFilesDeleteRule,
	)
}

const RuntimeAssetPackFilesDeleteRule = `
WITH previous(installation_id,path) AS (VALUES(?,?))
SELECT CASE
-- runtime_asset_pack_files_guarded_delete
WHEN (NOT EXISTS(
  SELECT 1 FROM runtime_asset_pack_installations installation
  WHERE installation.id=previous.installation_id AND installation.status='DELETE_PENDING'
)) THEN 'runtime pack file is immutable'
ELSE '' END
FROM previous`
