package recordstore

import (
	"context"
	"database/sql"

	dbapi "retrom/internal/database"
)

func CreateBiosInstallations(
	ctx context.Context, db dbapi.Executor, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "bios_installations", "id", ValidateBiosInstallations)
}

func ValidateBiosInstallations(ctx context.Context, db dbapi.Executor, keys ...any) error {
	return validate(ctx, db, bios_installationsOwnership, keys)
}

const bios_installationsOwnership = `
SELECT CASE
WHEN ((candidate.source_kind='BROWSER_UPLOAD' AND candidate.server_import_candidate_id IS NOT NULL)
  OR (candidate.source_kind='SERVER_DIRECTORY' AND (
    candidate.server_import_candidate_id IS NULL OR NOT EXISTS(
      SELECT 1 FROM server_bios_import_candidates source_candidate
      WHERE source_candidate.id=candidate.server_import_candidate_id
      AND source_candidate.requirement_id=candidate.requirement_id AND source_candidate.state='SELECTED'
    )
  ))) THEN 'invalid BIOS installation source'
ELSE '' END
FROM bios_installations candidate
WHERE candidate.id=?`
