package recordstore

import (
	"context"
	"database/sql"
)

func UpdateRpgmakerGameProfiles(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"rpgmaker_game_profiles",
		"game_id",
		RpgmakerGameProfilesUpdateRule,
	)
}

const RpgmakerGameProfilesUpdateRule = `
WITH previous(game_id) AS (VALUES(?))
SELECT CASE
-- rpgmaker_game_profiles_validate_update
WHEN (NOT EXISTS(
  SELECT 1 FROM games game
  WHERE game.id=candidate.game_id AND game.content_kind='RPG_MAKER_PROJECT'
    AND json_extract(game.source_manifest_json,'$.fileCount')=candidate.file_count
    AND json_extract(game.source_manifest_json,'$.totalBytes')=candidate.total_bytes
    AND json_extract(game.source_manifest_json,'$.filesDigest')=candidate.project_fingerprint
)) THEN 'RPG Maker content profile manifest mismatch'
ELSE '' END
FROM rpgmaker_game_profiles candidate CROSS JOIN previous
WHERE candidate.game_id=previous.game_id`
