package recordstore

import (
	"context"
	"database/sql"
)

func CreateUploadConsumptions(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateUploadConsumptions)
}

func ValidateUploadConsumptions(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, upload_consumptionsOwnership, keys)
}

const upload_consumptionsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM upload_sessions upload
  WHERE upload.id=candidate.upload_session_id AND (
    upload.purpose='GENERAL' AND candidate.consumer_type<>'RUNTIME_ASSET_PACK_INSTALLATION'
    OR upload.purpose='PROJECT'
      AND candidate.consumer_type IN ('IMPORT_JOB','GAME_CONTENT_REPLACE_JOB')
    OR upload.purpose='RUNTIME_ASSET_PACK'
      AND candidate.consumer_type='RUNTIME_ASSET_PACK_INSTALLATION'
      AND candidate.upload_file_id IS NULL
      AND EXISTS(SELECT 1 FROM runtime_asset_pack_installations installation
        WHERE installation.id=candidate.consumer_id)
  )
)) THEN 'upload purpose/consumer mismatch'
ELSE '' END
FROM upload_consumptions candidate
WHERE candidate.id=?`
