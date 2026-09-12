package recordstore

import (
	"context"
	"database/sql"
)

func UpdateIsolatedRuntimeCapabilities(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"isolated_runtime_capabilities",
		"credential_sha256,expected_origin,expires_at_ms,issued_at_ms,launch_id,preview_id,"+
			"profile_id,revoked_at_ms",
		IsolatedRuntimeCapabilitiesUpdateRule,
	)
}

const IsolatedRuntimeCapabilitiesUpdateRule = `
WITH previous(credential_sha256,expected_origin,expires_at_ms,issued_at_ms,launch_id,preview_id,
profile_id,revoked_at_ms) AS (VALUES(?,?,?,?,?,?,?,?))
SELECT CASE
-- isolated_runtime_capabilities_revoke
WHEN (previous.revoked_at_ms IS NOT NULL OR candidate.credential_sha256<>previous.credential_sha256
  OR candidate.launch_id IS NOT previous.launch_id OR candidate.preview_id IS NOT previous.preview_id
  OR candidate.profile_id<>previous.profile_id
  OR candidate.expected_origin<>previous.expected_origin OR candidate.issued_at_ms<>previous.issued_at_ms
  OR candidate.expires_at_ms<>previous.expires_at_ms OR candidate.revoked_at_ms IS NULL) THEN
'invalid isolated runtime capability revocation'
ELSE '' END
FROM isolated_runtime_capabilities candidate CROSS JOIN previous
WHERE candidate.credential_sha256=previous.credential_sha256`

func DeleteIsolatedRuntimeCapabilities(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"isolated_runtime_capabilities",
		"credential_sha256,launch_id,preview_id",
		IsolatedRuntimeCapabilitiesDeleteRule,
	)
}

const IsolatedRuntimeCapabilitiesDeleteRule = `
WITH previous(credential_sha256,launch_id,preview_id) AS (VALUES(?,?,?))
SELECT CASE
-- isolated_runtime_capabilities_immutable_delete
WHEN (previous.launch_id IS NOT NULL OR EXISTS(
  SELECT 1 FROM review_preview_sessions preview
  WHERE preview.id=previous.preview_id AND preview.state NOT IN ('EXPIRED','REVOKED')
)) THEN 'isolated runtime capability is retained for audit'
ELSE '' END
FROM previous`
