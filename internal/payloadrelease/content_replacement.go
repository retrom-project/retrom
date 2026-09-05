package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"
)

// ContentReplacementImpact carries the runtime rows retired by an in-place
// content replacement and the blobs that may have become unreferenced.
type ContentReplacementImpact struct {
	SaveStateCount   int64
	CandidateBlobIDs []string
}

// RetireCurrentGameContent invalidates runtime state bound to the content being
// replaced. The caller updates the game and selected variant in the same
// transaction, then stages CandidateBlobIDs after writing the new references.
func (service *Service) RetireCurrentGameContent(
	ctx context.Context,
	transaction *sql.Tx,
	gameID, selectedVariantID string,
	now int64,
) (ContentReplacementImpact, error) {
	blobIDs, err := currentGameContentBlobIDs(ctx, transaction, gameID)
	if err != nil {
		return ContentReplacementImpact{}, err
	}
	if err := stopCurrentGameRuntime(ctx, transaction, gameID, now); err != nil {
		return ContentReplacementImpact{}, err
	}
	result, err := transaction.ExecContext(ctx, `DELETE FROM save_states WHERE game_id=?`, gameID)
	if err != nil {
		return ContentReplacementImpact{}, fmt.Errorf("payloadrelease/delete replaced-content saves: %w", err)
	}
	saveCount, err := result.RowsAffected()
	if err != nil {
		return ContentReplacementImpact{}, fmt.Errorf("payloadrelease/count replaced-content saves: %w", err)
	}
	for _, statement := range currentGameContentDeleteStatements() {
		if err := execBatches(ctx, transaction, statement, gameID); err != nil {
			return ContentReplacementImpact{}, err
		}
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE game_variants
SET status='BLOCKED',compatibility_code='CONTENT_REPLACED',emulator_game_id=NULL,
    version=version+1,updated_at_ms=?
WHERE game_id=? AND id<>?
`, now, gameID, selectedVariantID); err != nil {
		return ContentReplacementImpact{}, fmt.Errorf("payloadrelease/block alternate variants: %w", err)
	}
	return ContentReplacementImpact{SaveStateCount: saveCount, CandidateBlobIDs: blobIDs}, nil
}

func currentGameContentBlobIDs(
	ctx context.Context,
	transaction *sql.Tx,
	gameID string,
) ([]string, error) {
	return collectIDs(ctx, transaction, `
SELECT file.blob_id FROM game_files file WHERE file.game_id=?
UNION ALL
SELECT file.source_archive_blob_id FROM game_files file WHERE file.game_id=?
UNION ALL
SELECT file.blob_id FROM variant_files file
JOIN game_variants variant ON variant.id=file.game_variant_id WHERE variant.game_id=?
UNION ALL
SELECT save.payload_blob_id FROM save_states save WHERE save.game_id=?
UNION ALL
SELECT save.screenshot_blob_id FROM save_states save WHERE save.game_id=?
UNION ALL
SELECT file.blob_id FROM launch_content_files file
JOIN launch_sessions launch ON launch.id=file.launch_session_id WHERE launch.game_id=?
UNION ALL
SELECT file.blob_id FROM launch_external_files file
JOIN launch_sessions launch ON launch.id=file.launch_session_id WHERE launch.game_id=?
`, gameID, gameID, gameID, gameID, gameID, gameID, gameID)
}

func stopCurrentGameRuntime(
	ctx context.Context,
	transaction *sql.Tx,
	gameID string,
	now int64,
) error {
	statements := []struct {
		query string
		args  []any
	}{
		{`UPDATE netplay_sessions SET state='FAILED',finished_at_ms=?,end_reason='GAME_CONTENT_REPLACED',
updated_at_ms=?,version=version+1 WHERE game_id=? AND state NOT IN ('FINISHED','FAILED')`, []any{now, now, gameID}},
		{`UPDATE netplay_rooms SET state='ENDED',current_session_id=NULL,ended_at_ms=?,
end_reason='GAME_CONTENT_REPLACED',updated_at_ms=?,version=version+1
WHERE selected_game_id=? AND state IN ('WAITING','STARTING','RUNNING')`, []any{now, now, gameID}},
		{`UPDATE launch_sessions SET save_state_id=NULL WHERE game_id=?`, []any{gameID}},
		{`UPDATE launch_sessions SET state='REVOKED',finished_at_ms=COALESCE(finished_at_ms,?),
updated_at_ms=?,version=version+1 WHERE game_id=? AND state IN ('CREATED','ACTIVE')`, []any{now, now, gameID}},
		{`UPDATE play_sessions SET state='ABANDONED',ended_at_ms=?,updated_at_ms=?,version=version+1
WHERE game_id=? AND state='ACTIVE'`, []any{now, now, gameID}},
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return fmt.Errorf("payloadrelease/stop replaced-content runtime: %w", err)
		}
	}
	return nil
}

func currentGameContentDeleteStatements() []string {
	return []string{
		`DELETE FROM launch_external_files WHERE rowid IN (
 SELECT file.rowid FROM launch_external_files file
 JOIN launch_sessions launch ON launch.id=file.launch_session_id
 WHERE launch.game_id=? ORDER BY file.rowid LIMIT 200
)`,
		`DELETE FROM launch_content_files WHERE rowid IN (
 SELECT file.rowid FROM launch_content_files file
 JOIN launch_sessions launch ON launch.id=file.launch_session_id
 WHERE launch.game_id=? ORDER BY file.rowid LIMIT 200
)`,
		`DELETE FROM variant_files WHERE rowid IN (
 SELECT file.rowid FROM variant_files file
 JOIN game_variants variant ON variant.id=file.game_variant_id
 WHERE variant.game_id=? ORDER BY file.rowid LIMIT 200
)`,
		`DELETE FROM variant_dependencies WHERE rowid IN (
 SELECT dependency.rowid FROM variant_dependencies dependency
 JOIN game_variants variant ON variant.id=dependency.game_variant_id
 WHERE variant.game_id=? ORDER BY dependency.rowid LIMIT 200
)`,
	}
}
