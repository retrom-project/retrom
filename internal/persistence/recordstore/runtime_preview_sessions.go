package recordstore

import (
	"context"
	"database/sql"

	dbapi "retrom/internal/database"
)

func CreateRuntimePreviewSessions(
	ctx context.Context, db dbapi.Executor, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "runtime_preview_sessions", "id", ValidateRuntimePreviewSessions)
}

func ValidateRuntimePreviewSessions(ctx context.Context, db dbapi.Executor, keys ...any) error {
	return validate(ctx, db, runtime_preview_sessionsOwnership, keys)
}

const runtime_preview_sessionsOwnership = `
SELECT CASE
WHEN (candidate.restore_from_preview_id IS NOT NULL AND NOT EXISTS(
 SELECT 1 FROM runtime_preview_sessions source
 WHERE source.id=candidate.restore_from_preview_id AND source.id<>candidate.id AND
source.actor_user_id=candidate.actor_user_id
 AND source.scope_id=candidate.scope_id AND
source.content_revision=candidate.content_revision
 AND source.provider_id=candidate.provider_id AND source.target_id=candidate.target_id
 AND json_extract(source.checkpoint_payload_file_record,'$.sha256')=
 json_extract(candidate.restore_payload_file_record,'$.sha256')
 AND json_extract(source.checkpoint_payload_file_record,'$.size_bytes')=
 json_extract(candidate.restore_payload_file_record,'$.size_bytes')
 AND json_extract(candidate.restore_payload_file_record,'$.path') LIKE 'previews/'||candidate.id||'/restore/%'
 AND source.checkpoint_format=candidate.restore_checkpoint_format
 AND source.state IN ('ACTIVE','FINISHED') AND source.hard_expires_at_ms>candidate.created_at_ms
)) THEN 'invalid preview restore source'
WHEN (NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  JOIN runtime_providers provider ON provider.provider_id=target.provider_id
  WHERE target.provider_id=candidate.provider_id AND target.target_id=candidate.target_id
    AND provider.bundle_sha256=candidate.bundle_sha256
)) THEN 'invalid runtime target snapshot'
ELSE '' END
FROM runtime_preview_sessions candidate
WHERE candidate.id=?`
