package recordstore

import (
	"context"
	"database/sql"
)

func CreateEmulationstationImports(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateEmulationstationImports)
}

func ValidateEmulationstationImports(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, emulationstation_importsOwnership, keys)
}

const emulationstation_importsOwnership = `
SELECT CASE
WHEN (candidate.state<>'SCANNING' OR candidate.phase<>'DISCOVERING_GAMELISTS'
  OR candidate.source_snapshot_digest IS NOT NULL OR candidate.import_job_id IS NOT NULL
  OR candidate.scan_completed_at_ms IS NOT NULL OR candidate.started_at_ms IS NOT NULL OR
candidate.completed_at_ms IS NOT NULL
  OR candidate.gamelist_count<>0 OR candidate.invalid_gamelist_count<>0 OR candidate.collection_count<>0
  OR candidate.folder_entry_count<>0 OR candidate.game_count<>0 OR candidate.estimated_source_bytes<>0
  OR candidate.mapped_collection_count<>0 OR candidate.skipped_collection_count<>0
  OR candidate.skipped_mapping_item_count<>0 OR candidate.processable_item_count<>0 OR
candidate.blocked_item_count<>0
  OR candidate.review_pending_item_count<>0 OR candidate.published_item_count<>0 OR
candidate.review_discarded_item_count<>0
  OR candidate.existing_item_count<>0 OR candidate.failed_item_count<>0 OR
candidate.cancelled_item_count<>0
  OR candidate.media_warning_count<>0 OR candidate.discovered_cover_count<>0 OR
candidate.discovered_video_count<>0) THEN 'invalid initial EmulationStation import'
WHEN (NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=candidate.scan_job_id
  AND job.kind='SERVER_EMULATIONSTATION_SCAN'
  AND job.scope_type='EMULATIONSTATION_IMPORT' AND job.scope_id=candidate.id
)) THEN 'invalid EmulationStation scan job'
ELSE '' END
FROM emulationstation_imports candidate
WHERE candidate.id=?`
