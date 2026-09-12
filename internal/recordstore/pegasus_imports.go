package recordstore

import (
	"context"
	"database/sql"
)

func CreatePegasusImports(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidatePegasusImports)
}

func ValidatePegasusImports(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, pegasus_importsOwnership, keys)
}

const pegasus_importsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=candidate.scan_job_id AND job.kind='SERVER_PEGASUS_SCAN'
  AND job.scope_type='PEGASUS_IMPORT' AND job.scope_id=candidate.id
)) THEN 'invalid Pegasus scan job'
ELSE '' END
FROM pegasus_imports candidate
WHERE candidate.id=?`
