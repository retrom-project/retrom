package recordstore

import (
	"context"
	"database/sql"

	"retrom/internal/dbexec"
)

func CreateDatVersions(
	ctx context.Context, db dbexec.Executor, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateDatVersions)
}

func ValidateDatVersions(ctx context.Context, db dbexec.Executor, keys ...any) error {
	return validate(ctx, db, dat_versionsOwnership, keys)
}

const dat_versionsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  WHERE target.provider_id=candidate.provider_id AND target.target_id=candidate.target_id
)) THEN 'invalid runtime target snapshot'
ELSE '' END
FROM dat_versions candidate
WHERE candidate.id=?`
