package recordstore

import (
	"context"
	"database/sql"
)

func CreateIsolatedRuntimeCapabilities(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "credential_sha256", ValidateIsolatedRuntimeCapabilities)
}

func ValidateIsolatedRuntimeCapabilities(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, isolated_runtime_capabilitiesOwnership, keys)
}

const isolated_runtime_capabilitiesOwnership = `
SELECT CASE
WHEN (NOT (
  candidate.launch_id IS NOT NULL AND EXISTS(
    SELECT 1 FROM launch_sessions launch
    JOIN isolated_runtime_bootstrap_tickets ticket ON ticket.launch_id=launch.id
    WHERE launch.id=candidate.launch_id AND launch.profile_id=candidate.profile_id
      AND launch.state IN ('CREATED','ACTIVE')
      AND ticket.profile_id=candidate.profile_id AND ticket.expected_origin=candidate.expected_origin
      AND ticket.consumed_at_ms IS NOT NULL AND candidate.issued_at_ms=ticket.consumed_at_ms
      AND candidate.issued_at_ms<=ticket.expires_at_ms AND
candidate.expires_at_ms<=launch.hard_expires_at_ms
      AND candidate.revoked_at_ms IS NULL
  )
  OR candidate.preview_id IS NOT NULL AND EXISTS(
    SELECT 1 FROM review_preview_sessions preview
    JOIN users actor ON actor.id=preview.actor_user_id
    JOIN isolated_runtime_bootstrap_tickets ticket ON ticket.preview_id=preview.id
    WHERE preview.id=candidate.preview_id AND actor.profile_id=candidate.profile_id
      AND preview.state IN ('CREATED','ACTIVE')
      AND ticket.profile_id=candidate.profile_id AND ticket.expected_origin=candidate.expected_origin
      AND ticket.consumed_at_ms IS NOT NULL AND candidate.issued_at_ms=ticket.consumed_at_ms
      AND candidate.issued_at_ms<=ticket.expires_at_ms AND
candidate.expires_at_ms<=preview.hard_expires_at_ms
      AND candidate.revoked_at_ms IS NULL
  )
)) THEN 'invalid isolated runtime capability'
ELSE '' END
FROM isolated_runtime_capabilities candidate
WHERE candidate.credential_sha256=?`
