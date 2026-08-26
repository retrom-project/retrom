package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"
)

type ContentReplacementImpact struct {
	SaveStateCount int64
}

// RetireSupersededGameContent removes every payload reference bound to content
// revisions that are no longer current. Revision rows remain as text audit data.
func (service *Service) RetireSupersededGameContent(
	ctx context.Context,
	transaction *sql.Tx,
	gameID, currentContentID string,
	now int64,
) (ContentReplacementImpact, error) {
	blobIDs, err := supersededContentBlobIDs(ctx, transaction, gameID, currentContentID)
	if err != nil {
		return ContentReplacementImpact{}, err
	}
	if err := stopSupersededContentRuntime(ctx, transaction, gameID, currentContentID, now); err != nil {
		return ContentReplacementImpact{}, err
	}
	result, err := transaction.ExecContext(ctx, `
DELETE FROM save_states
WHERE game_id=? AND game_variant_revision_id IN (
  SELECT revision.id FROM game_variant_revisions revision
  JOIN game_variants variant ON variant.id=revision.game_variant_id
  WHERE variant.game_id=? AND revision.game_content_revision_id<>?
)
`, gameID, gameID, currentContentID)
	if err != nil {
		return ContentReplacementImpact{}, fmt.Errorf("payloadrelease/delete superseded saves: %w", err)
	}
	saveCount, err := result.RowsAffected()
	if err != nil {
		return ContentReplacementImpact{}, fmt.Errorf("payloadrelease/count superseded saves: %w", err)
	}
	for _, statement := range supersededContentDeleteStatements() {
		if err := execBatches(ctx, transaction, statement, gameID, currentContentID); err != nil {
			return ContentReplacementImpact{}, err
		}
	}
	if err := service.stageCandidates(ctx, transaction, blobIDs); err != nil {
		return ContentReplacementImpact{}, err
	}
	return ContentReplacementImpact{SaveStateCount: saveCount}, nil
}

func supersededContentBlobIDs(
	ctx context.Context,
	transaction *sql.Tx,
	gameID, currentContentID string,
) ([]string, error) {
	return collectIDs(ctx, transaction, `
SELECT file.blob_id FROM game_content_files file
JOIN game_content_revisions revision ON revision.id=file.game_content_revision_id
WHERE revision.game_id=? AND revision.id<>?
UNION ALL
SELECT file.source_archive_blob_id FROM game_content_files file
JOIN game_content_revisions revision ON revision.id=file.game_content_revision_id
WHERE revision.game_id=? AND revision.id<>?
UNION ALL
SELECT file.blob_id FROM variant_files file
JOIN game_variant_revisions revision ON revision.id=file.game_variant_revision_id
JOIN game_variants variant ON variant.id=revision.game_variant_id
WHERE variant.game_id=? AND revision.game_content_revision_id<>?
UNION ALL
SELECT save.payload_blob_id FROM save_states save
JOIN game_variant_revisions revision ON revision.id=save.game_variant_revision_id
WHERE save.game_id=? AND revision.game_content_revision_id<>?
UNION ALL
SELECT save.screenshot_blob_id FROM save_states save
JOIN game_variant_revisions revision ON revision.id=save.game_variant_revision_id
WHERE save.game_id=? AND revision.game_content_revision_id<>?
UNION ALL
SELECT file.blob_id FROM launch_content_files file
JOIN launch_sessions launch ON launch.id=file.launch_session_id
JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
WHERE launch.game_id=? AND revision.game_content_revision_id<>?
UNION ALL
SELECT file.blob_id FROM launch_external_files file
JOIN launch_sessions launch ON launch.id=file.launch_session_id
JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
WHERE launch.game_id=? AND revision.game_content_revision_id<>?
`, gameID, currentContentID, gameID, currentContentID, gameID, currentContentID,
		gameID, currentContentID, gameID, currentContentID, gameID, currentContentID,
		gameID, currentContentID)
}

func stopSupersededContentRuntime(
	ctx context.Context,
	transaction *sql.Tx,
	gameID, currentContentID string,
	now int64,
) error {
	statements := []struct {
		query string
		args  []any
	}{
		{`UPDATE netplay_sessions SET state='FAILED',finished_at_ms=?,end_reason='GAME_CONTENT_REPLACED',
updated_at_ms=?,version=version+1 WHERE game_id=? AND state NOT IN ('FINISHED','FAILED')
AND game_variant_revision_id IN (
 SELECT revision.id FROM game_variant_revisions revision JOIN game_variants variant
 ON variant.id=revision.game_variant_id WHERE variant.game_id=? AND revision.game_content_revision_id<>?
)`, []any{now, now, gameID, gameID, currentContentID}},
		{`UPDATE netplay_rooms SET state='ENDED',current_session_id=NULL,ended_at_ms=?,
end_reason='GAME_CONTENT_REPLACED',updated_at_ms=?,version=version+1
WHERE selected_game_id=? AND state IN ('WAITING','STARTING','RUNNING')
AND selected_game_variant_revision_id IN (
 SELECT revision.id FROM game_variant_revisions revision JOIN game_variants variant
 ON variant.id=revision.game_variant_id WHERE variant.game_id=? AND revision.game_content_revision_id<>?
)`, []any{now, now, gameID, gameID, currentContentID}},
		{`UPDATE launch_sessions SET save_state_id=NULL WHERE game_id=? AND save_state_id IN (
 SELECT save.id FROM save_states save JOIN game_variant_revisions revision
 ON revision.id=save.game_variant_revision_id WHERE save.game_id=? AND revision.game_content_revision_id<>?
)`, []any{gameID, gameID, currentContentID}},
		{`UPDATE launch_sessions SET state='REVOKED',finished_at_ms=COALESCE(finished_at_ms,?),
updated_at_ms=?,version=version+1 WHERE game_id=? AND state IN ('CREATED','ACTIVE')
AND game_variant_revision_id IN (
 SELECT revision.id FROM game_variant_revisions revision JOIN game_variants variant
 ON variant.id=revision.game_variant_id WHERE variant.game_id=? AND revision.game_content_revision_id<>?
)`, []any{now, now, gameID, gameID, currentContentID}},
		{`UPDATE play_sessions SET state='ABANDONED',ended_at_ms=?,updated_at_ms=?,version=version+1
WHERE game_id=? AND state='ACTIVE' AND game_variant_revision_id IN (
 SELECT revision.id FROM game_variant_revisions revision JOIN game_variants variant
 ON variant.id=revision.game_variant_id WHERE variant.game_id=? AND revision.game_content_revision_id<>?
)`, []any{now, now, gameID, gameID, currentContentID}},
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return fmt.Errorf("payloadrelease/stop superseded content runtime: %w", err)
		}
	}
	return nil
}

func supersededContentDeleteStatements() []string {
	return []string{
		`DELETE FROM launch_external_files WHERE rowid IN (
 SELECT file.rowid FROM launch_external_files file
 JOIN launch_sessions launch ON launch.id=file.launch_session_id
 JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
 WHERE launch.game_id=? AND revision.game_content_revision_id<>? ORDER BY file.rowid LIMIT 200
)`,
		`DELETE FROM launch_content_files WHERE rowid IN (
 SELECT file.rowid FROM launch_content_files file
 JOIN launch_sessions launch ON launch.id=file.launch_session_id
 JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
 WHERE launch.game_id=? AND revision.game_content_revision_id<>? ORDER BY file.rowid LIMIT 200
)`,
		`DELETE FROM variant_files WHERE rowid IN (
 SELECT file.rowid FROM variant_files file
 JOIN game_variant_revisions revision ON revision.id=file.game_variant_revision_id
 JOIN game_variants variant ON variant.id=revision.game_variant_id
 WHERE variant.game_id=? AND revision.game_content_revision_id<>? ORDER BY file.rowid LIMIT 200
)`,
		`DELETE FROM game_content_files WHERE rowid IN (
 SELECT file.rowid FROM game_content_files file
 JOIN game_content_revisions revision ON revision.id=file.game_content_revision_id
 WHERE revision.game_id=? AND revision.id<>? ORDER BY file.rowid LIMIT 200
)`,
	}
}
