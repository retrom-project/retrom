package recordstore

import (
	"context"
	"database/sql"
)

func CreateImportJobs(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateImportJobs)
}

func ValidateImportJobs(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, import_jobsOwnership, keys)
}

const import_jobsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  WHERE target.provider_id=candidate.provider_id AND target.target_id=candidate.target_id
)) THEN 'invalid runtime target snapshot'
ELSE '' END
FROM import_jobs candidate
WHERE candidate.id=?`
