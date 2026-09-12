package recordstore

import (
	"context"
	"database/sql"
)

func CreateImportItemCoreValidations(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateImportItemCoreValidations)
}

func ValidateImportItemCoreValidations(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, import_item_core_validationsOwnership, keys)
}

const import_item_core_validationsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM import_item_source_snapshots snapshot
  WHERE snapshot.id=candidate.source_snapshot_id
  AND snapshot.import_item_id=candidate.import_item_id
  AND snapshot.source_manifest_digest=candidate.source_manifest_digest
)) THEN 'invalid validation source snapshot'
WHEN (NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  WHERE target.provider_id=candidate.provider_id AND target.target_id=candidate.target_id
)) THEN 'invalid runtime target snapshot'
ELSE '' END
FROM import_item_core_validations candidate
WHERE candidate.id=?`
