package recordstore

import (
	"context"
	"database/sql"

	"retrom/internal/dbexec"
)

func CreateSourceImports(
	ctx context.Context, db dbexec.Executor, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateSourceImports)
}

func ValidateSourceImports(ctx context.Context, db dbexec.Executor, keys ...any) error {
	return validate(ctx, db, source_importsOwnership, keys)
}

const source_importsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=candidate.scan_job_id AND job.kind='IMPORT_SCAN'
  AND job.scope_type='SOURCE_IMPORT' AND job.scope_id=candidate.id
)) THEN 'invalid Source scan job'
ELSE '' END
FROM source_imports candidate
WHERE candidate.id=?`
