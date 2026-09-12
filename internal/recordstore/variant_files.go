package recordstore

import (
	"context"
	"database/sql"
)

func CreateVariantFiles(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "game_variant_id,role,logical_name", ValidateVariantFiles)
}

func ValidateVariantFiles(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, variant_filesOwnership, keys)
}

const variant_filesOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM game_variants variant
  JOIN games game ON game.id=variant.game_id
  WHERE variant.id=candidate.game_variant_id AND game.status='PUBLISHED'
)) THEN 'game payload owner is not published'
ELSE '' END
FROM variant_files candidate
WHERE candidate.game_variant_id=? AND candidate.role=? AND candidate.logical_name=?`
