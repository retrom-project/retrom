package recordstore

import (
	"context"
	"database/sql"
)

func CreateServerImports(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateServerImports)
}

func ValidateServerImports(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, server_importsOwnership, keys)
}

const server_importsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=candidate.job_id AND job.kind='SERVER_BIOS_IMPORT'
  AND job.scope_type='SERVER_IMPORT' AND job.scope_id=candidate.id
)) THEN 'invalid server import job'
ELSE '' END
FROM server_imports candidate
WHERE candidate.id=?`
