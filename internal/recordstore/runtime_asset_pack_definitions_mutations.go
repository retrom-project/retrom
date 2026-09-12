package recordstore

import (
	"context"
	"database/sql"
)

func UpdateRuntimeAssetPackDefinitions(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"runtime_asset_pack_definitions",
		"id,created_at_ms,created_by_user_id,declared_name,generation,kind,"+
			"normalized_declared_name,origin,required_layout_version",
		RuntimeAssetPackDefinitionsUpdateRule,
	)
}

const RuntimeAssetPackDefinitionsUpdateRule = `
WITH previous(id,created_at_ms,created_by_user_id,declared_name,generation,kind,normalized_declared_name,
origin,required_layout_version) AS (VALUES(?,?,?,?,?,?,?,?,?))
SELECT CASE
-- runtime_asset_pack_definitions_guarded_update
WHEN (candidate.id IS NOT previous.id OR candidate.origin IS NOT previous.origin
  OR candidate.created_by_user_id IS NOT previous.created_by_user_id OR candidate.created_at_ms IS NOT
previous.created_at_ms
  OR (EXISTS(SELECT 1 FROM runtime_asset_pack_installations WHERE definition_id=previous.id)
    AND (candidate.kind IS NOT previous.kind OR candidate.generation IS NOT previous.generation
      OR candidate.declared_name IS NOT previous.declared_name
      OR candidate.normalized_declared_name IS NOT previous.normalized_declared_name
      OR candidate.required_layout_version IS NOT previous.required_layout_version))) THEN
'runtime pack definition is immutable'
ELSE '' END
FROM runtime_asset_pack_definitions candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
