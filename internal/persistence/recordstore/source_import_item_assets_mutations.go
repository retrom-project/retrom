package recordstore

import (
	"context"
	"database/sql"

	"retrom/internal/dbexec"
)

func UpdateSourceImportItemAssets(
	ctx context.Context, db dbexec.Executor, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"source_import_item_assets",
		"item_id,kind,created_at_ms,relative_path,resolution_method,size_bytes,"+
			"source_facts_digest",
		SourceImportItemAssetsUpdateRule,
	)
}

const SourceImportItemAssetsUpdateRule = `
WITH previous(item_id,kind,created_at_ms,relative_path,resolution_method,size_bytes,source_facts_digest)
AS (VALUES(?,?,?,?,?,?,?))
SELECT CASE
-- source_asset_snapshot_update
WHEN (candidate.item_id<>previous.item_id OR candidate.kind<>previous.kind OR
candidate.resolution_method<>previous.resolution_method OR
  candidate.relative_path<>previous.relative_path OR candidate.size_bytes IS NOT previous.size_bytes OR
  candidate.source_facts_digest IS NOT previous.source_facts_digest OR
candidate.created_at_ms<>previous.created_at_ms) THEN 'immutable Source asset snapshot'
ELSE '' END
FROM source_import_item_assets candidate CROSS JOIN previous
WHERE candidate.item_id=previous.item_id AND candidate.kind=previous.kind`
