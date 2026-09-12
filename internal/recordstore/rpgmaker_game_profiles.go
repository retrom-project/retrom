package recordstore

import (
	"context"
	"database/sql"
)

func CreateRpgmakerGameProfiles(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "game_id", ValidateRpgmakerGameProfiles)
}

func ValidateRpgmakerGameProfiles(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, rpgmaker_game_profilesOwnership, keys)
}

const rpgmaker_game_profilesOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM games game
  WHERE game.id=candidate.game_id AND game.content_kind='RPG_MAKER_PROJECT'
    AND json_extract(game.source_manifest_json,'$.fileCount')=candidate.file_count
    AND json_extract(game.source_manifest_json,'$.totalBytes')=candidate.total_bytes
    AND json_extract(game.source_manifest_json,'$.filesDigest')=candidate.project_fingerprint
)) THEN 'RPG Maker content profile manifest mismatch'
ELSE '' END
FROM rpgmaker_game_profiles candidate
WHERE candidate.game_id=?`
