package recordstore

import (
	"context"
	"database/sql"
)

func UpdateRuntimeAssetPackInstallations(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"runtime_asset_pack_installations",
		"id,bundle_blob_id,bundle_sha256,created_at_ms,created_by_user_id,definition_id,"+
			"deleted_at_ms,diagnostic_json,file_count,files_digest,source_note,status,total_bytes,"+
			"validated_at_ms,version",
		RuntimeAssetPackInstallationsUpdateRule,
	)
}

const RuntimeAssetPackInstallationsUpdateRule = `
WITH previous(id,bundle_blob_id,bundle_sha256,created_at_ms,created_by_user_id,definition_id,
deleted_at_ms,diagnostic_json,file_count,files_digest,source_note,status,total_bytes,validated_at_ms,
version) AS (VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?))
SELECT CASE
-- runtime_asset_pack_installations_diagnostic_update
WHEN ((candidate.diagnostic_json IS NOT previous.diagnostic_json) AND (previous.status<>'VALIDATING'))
THEN 'terminal runtime pack diagnostic is immutable'
-- runtime_asset_pack_installations_identity_update
WHEN ((candidate.definition_id IS NOT previous.definition_id OR candidate.files_digest IS NOT
previous.files_digest OR candidate.file_count IS NOT previous.file_count OR candidate.total_bytes IS NOT
previous.total_bytes OR candidate.source_note IS NOT previous.source_note OR
candidate.created_by_user_id IS NOT previous.created_by_user_id OR candidate.created_at_ms IS NOT
previous.created_at_ms) AND (1=1)) THEN 'runtime pack installation identity is immutable'
-- runtime_asset_pack_installations_state_update
WHEN ((candidate.status IS NOT previous.status OR candidate.bundle_blob_id IS NOT
previous.bundle_blob_id OR candidate.bundle_sha256 IS NOT previous.bundle_sha256 OR
candidate.validated_at_ms IS NOT previous.validated_at_ms OR candidate.deleted_at_ms IS NOT
previous.deleted_at_ms) AND (NOT (
  previous.status='VALIDATING' AND candidate.status IN ('READY','FAILED')
    AND candidate.bundle_blob_id IS previous.bundle_blob_id AND candidate.bundle_sha256 IS
previous.bundle_sha256
    AND (candidate.status<>'READY' OR (
      candidate.bundle_blob_id IS NOT NULL
      AND candidate.file_count=(SELECT count(*) FROM runtime_asset_pack_files file WHERE
file.installation_id=previous.id)
      AND candidate.total_bytes=(SELECT COALESCE(sum(file.size_bytes),0)
        FROM runtime_asset_pack_files file WHERE file.installation_id=previous.id)
    ))
  OR previous.status IN ('READY','FAILED') AND candidate.status='DELETE_PENDING'
    AND candidate.bundle_blob_id IS previous.bundle_blob_id AND candidate.bundle_sha256 IS
previous.bundle_sha256
  OR previous.status='DELETE_PENDING' AND candidate.status='DELETED'
    AND candidate.bundle_blob_id IS NULL AND candidate.bundle_sha256 IS NULL
    AND NOT EXISTS(SELECT 1 FROM runtime_asset_pack_files file WHERE file.installation_id=previous.id)
)
OR candidate.status='DELETE_PENDING' AND EXISTS(
  SELECT 1 FROM game_variant_runtime_packs reference
  WHERE reference.installation_id=previous.id
)
OR candidate.bundle_blob_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM blobs blob WHERE blob.id=candidate.bundle_blob_id AND blob.sha256=candidate.bundle_sha256
))) THEN 'invalid runtime pack installation transition'
-- runtime_asset_pack_installations_version_update
WHEN (candidate.version<>previous.version+1) THEN
'runtime pack installation version must increment by one'
ELSE '' END
FROM runtime_asset_pack_installations candidate CROSS JOIN previous
WHERE candidate.id=previous.id`

func DeleteRuntimeAssetPackInstallations(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"runtime_asset_pack_installations",
		"id",
		RuntimeAssetPackInstallationsDeleteRule,
	)
}

const RuntimeAssetPackInstallationsDeleteRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- runtime_asset_pack_installations_immutable_delete
WHEN (1=1) THEN 'runtime pack installation is retained for audit'
ELSE '' END
FROM previous`
