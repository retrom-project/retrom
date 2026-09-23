package recordstore

import (
	"context"
	"database/sql"

	"retrom/internal/dbexec"
)

func CreateUploadConsumptions(
	ctx context.Context, db dbexec.Executor, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateUploadConsumptions)
}

func ValidateUploadConsumptions(ctx context.Context, db dbexec.Executor, keys ...any) error {
	return validate(ctx, db, upload_consumptionsOwnership, keys)
}

const upload_consumptionsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM upload_sessions upload
  WHERE upload.id=candidate.upload_session_id AND (
    upload.purpose='GENERAL'
    OR upload.purpose='PROJECT'
      AND candidate.consumer_type IN ('IMPORT_JOB','GAME_CONTENT_REPLACE_JOB')
  )
)) THEN 'upload purpose/consumer mismatch'
ELSE '' END
FROM upload_consumptions candidate
WHERE candidate.id=?`
