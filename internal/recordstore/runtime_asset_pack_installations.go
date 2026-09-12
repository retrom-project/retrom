package recordstore

import (
	"context"
	"database/sql"
)

func CreateRuntimeAssetPackInstallations(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateRuntimeAssetPackInstallations)
}

func ValidateRuntimeAssetPackInstallations(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, runtime_asset_pack_installationsOwnership, keys)
}

const runtime_asset_pack_installationsOwnership = `
SELECT CASE
WHEN (candidate.bundle_blob_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM blobs blob WHERE blob.id=candidate.bundle_blob_id AND blob.sha256=candidate.bundle_sha256
)) THEN 'runtime pack bundle blob mismatch'
ELSE '' END
FROM runtime_asset_pack_installations candidate
WHERE candidate.id=?`
