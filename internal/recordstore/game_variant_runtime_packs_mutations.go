package recordstore

import (
	"context"
	"database/sql"
)

func UpdateGameVariantRuntimePacks(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"game_variant_runtime_packs",
		"game_variant_id,slot",
		GameVariantRuntimePacksUpdateRule,
	)
}

const GameVariantRuntimePacksUpdateRule = `
WITH previous(game_variant_id,slot) AS (VALUES(?,?))
SELECT CASE
-- game_variant_runtime_packs_validate_update
WHEN (NOT EXISTS(
  SELECT 1 FROM rpgmaker_variant_profiles profile
  JOIN runtime_asset_pack_definitions definition ON definition.id=candidate.definition_id
  JOIN runtime_asset_pack_installations installation
    ON installation.id=candidate.installation_id AND installation.definition_id=definition.id
  WHERE profile.game_variant_id=candidate.game_variant_id
    AND definition.enabled=1 AND definition.generation=profile.generation
    AND definition.declared_name=candidate.declared_name
    AND definition.normalized_declared_name=candidate.normalized_declared_name
    AND installation.status='READY'
    AND installation.file_count=(SELECT count(*) FROM runtime_asset_pack_files file
      WHERE file.installation_id=installation.id)
    AND installation.total_bytes=(SELECT COALESCE(sum(file.size_bytes),0) FROM runtime_asset_pack_files
file
      WHERE file.installation_id=installation.id)
    AND (
      profile.generation IN ('RPG2000','RPG2003') AND candidate.slot=0
      OR profile.generation IN ('RPGXP','RPGVX','RPGVXACE') AND candidate.slot BETWEEN 1 AND 3
    )
)) THEN 'invalid variant runtime pack'
ELSE '' END
FROM game_variant_runtime_packs candidate CROSS JOIN previous
WHERE candidate.game_variant_id=previous.game_variant_id AND candidate.slot=previous.slot`
