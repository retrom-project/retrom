package recordstore

import (
	"context"
	"database/sql"
)

func CreateRuntimeAssetPackFiles(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "installation_id,path", ValidateRuntimeAssetPackFiles)
}

func ValidateRuntimeAssetPackFiles(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, runtime_asset_pack_filesOwnership, keys)
}

const runtime_asset_pack_filesOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM runtime_asset_pack_installations installation
  WHERE installation.id=candidate.installation_id AND installation.status='VALIDATING'
)
OR NOT EXISTS(
  SELECT 1 FROM blobs blob
  WHERE blob.id=candidate.blob_id AND blob.size_bytes=candidate.size_bytes AND
blob.sha256=candidate.sha256
)
OR candidate.ordinal<>(SELECT count(*) FROM runtime_asset_pack_files file WHERE
file.installation_id=candidate.installation_id AND file.ordinal<candidate.ordinal)
OR candidate.ordinal>0 AND candidate.path<=CAST((
  SELECT file.path FROM runtime_asset_pack_files file
  WHERE file.installation_id=candidate.installation_id AND file.ordinal=candidate.ordinal-1
) AS TEXT)) THEN 'invalid runtime pack file'
ELSE '' END
FROM runtime_asset_pack_files candidate
WHERE candidate.installation_id=? AND candidate.path=?`
