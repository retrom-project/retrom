package recordstore

import (
	"context"
	"database/sql"

	dbapi "retrom/internal/database"
)

func CreateVariantFiles(
	ctx context.Context, db dbapi.Executor, query string, args ...any,
) (sql.Result, error) {
	return create(
		ctx,
		db,
		query,
		args,
		"variant_files",
		"game_variant_id,role,logical_name",
		ValidateVariantFiles,
	)
}

func ValidateVariantFiles(ctx context.Context, db dbapi.Executor, keys ...any) error {
	return validate(ctx, db, variant_filesOwnership, keys)
}

func UpsertVariantFiles(
	ctx context.Context, db dbapi.Executor, query string, args ...any,
) (sql.Result, error) {
	return upsertRecords(
		ctx,
		db,
		"variant_files",
		query,
		args,
		"game_variant_id,role,logical_name",
		ValidateVariantFiles,
	)
}

const variant_filesOwnership = `
SELECT CASE
WHEN NOT (candidate.file_record IS JSON) THEN 'invalid variant file record'
WHEN candidate.role<>'BIOS_BUNDLE' AND NOT EXISTS(SELECT 1 FROM game_variants variant
 WHERE variant.id=candidate.game_variant_id AND ((candidate.file_record)::jsonb #>> '{path}') LIKE
 'files/' || right(variant.game_id,2) || '/' || variant.game_id || '/%')
 THEN 'variant file is outside its game directory'
WHEN (NOT EXISTS(
  SELECT 1 FROM game_variants variant
  JOIN games game ON game.id=variant.game_id
  WHERE variant.id=candidate.game_variant_id AND game.status='PUBLISHED'
)) THEN 'game payload owner is not published'
ELSE '' END
FROM variant_files candidate
WHERE candidate.game_variant_id=? AND candidate.role=? AND candidate.logical_name=?`
