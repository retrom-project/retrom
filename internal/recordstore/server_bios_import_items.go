package recordstore

import (
	"context"
	"database/sql"
)

func CreateServerBiosImportItems(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "server_import_id,requirement_id", ValidateServerBiosImportItems)
}

func ValidateServerBiosImportItems(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, server_bios_import_itemsOwnership, keys)
}

const server_bios_import_itemsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  WHERE target.provider_id=candidate.provider_id AND target.target_id=candidate.target_id
)) THEN 'invalid runtime target snapshot'
ELSE '' END
FROM server_bios_import_items candidate
WHERE candidate.server_import_id=? AND candidate.requirement_id=?`
