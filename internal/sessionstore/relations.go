package sessionstore

import (
	"context"
	"fmt"

	"retrom/internal/recordstore"
)

func createLaunchRelations(ctx context.Context, tx recordstore.DBTX, id string) error {
	if err := recordstore.ValidateLaunchSessions(ctx, tx, id); err != nil {
		return fmt.Errorf("validate session record: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO launch_payload_retirements(launch_session_id,due_at_ms)
SELECT id,`+retirementDeadline+` FROM launch_sessions WHERE id=?`, id); err != nil {
		return fmt.Errorf("schedule launch retirement: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO launch_game_save_bindings(
launch_session_id,save_state_id,expected_data_version,restore_payload_blob_id,
restore_checkpoint_format,initial_active_duration_ms)
SELECT launch.id,save.id,COALESCE(native.data_version,0),save.payload_blob_id,
save.checkpoint_format,COALESCE(save.active_duration_ms,0)
FROM launch_sessions launch
JOIN runtime_targets target ON target.provider_id=launch.provider_id AND target.target_id=launch.target_id
LEFT JOIN save_states save ON save.id=launch.save_state_id AND save.profile_id=launch.profile_id
AND save.game_id=launch.game_id AND save.deleted_at_ms IS NULL
LEFT JOIN game_save_versions native ON native.save_state_id=save.id
WHERE launch.id=? AND launch.game_id IS NOT NULL
AND json_extract(target.checkpoint_json,'$.semantics')='GAME_SAVE'`, id); err != nil {
		return fmt.Errorf("freeze launch restore input: %w", err)
	}
	return nil
}

// CASE keeps scalar min semantics independent of SQLite's min(a,b) function.
const retirementDeadline = `CASE
WHEN state IN ('FINISHED','EXPIRED','REVOKED') THEN finished_at_ms
WHEN state='CREATED' THEN CASE WHEN bootstrap_expires_at_ms<hard_expires_at_ms
THEN bootstrap_expires_at_ms ELSE hard_expires_at_ms END
WHEN idle_expires_at_ms<hard_expires_at_ms THEN idle_expires_at_ms
ELSE hard_expires_at_ms END`

func updateLaunchRelations(ctx context.Context, tx recordstore.DBTX, id string) error {
	if _, err := tx.ExecContext(ctx, `
UPDATE launch_payload_retirements SET due_at_ms=(SELECT `+retirementDeadline+`
FROM launch_sessions WHERE id=?) WHERE launch_session_id=? AND released_at_ms IS NULL`, id, id); err != nil {
		return fmt.Errorf("update launch lifecycle relation: %w", err)
	}
	if _, err := recordstore.UpdateIsolatedRuntimeCapabilities(ctx, tx, recordstore.Update{
		Set: `
revoked_at_ms=(
SELECT finished_at_ms FROM launch_sessions WHERE id=?)
`,
		Scope: recordstore.Scope{
			Where: `
launch_id=? AND revoked_at_ms IS NULL
AND EXISTS(SELECT 1 FROM launch_sessions launch WHERE launch.id=launch_id
AND launch.state IN ('FINISHED','EXPIRED','REVOKED'))
`,
			Args: []any{id},
		},
		Values: []any{id},
	}); err != nil {
		return fmt.Errorf("update launch lifecycle relation: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE launch_game_save_bindings SET restore_payload_blob_id=NULL,restore_checkpoint_format=NULL
WHERE launch_session_id=? AND EXISTS(SELECT 1 FROM launch_sessions WHERE id=?
AND state NOT IN ('CREATED','ACTIVE'))`, id, id); err != nil {
		return fmt.Errorf("update launch lifecycle relation: %w", err)
	}
	return nil
}

func createSaveVersion(ctx context.Context, tx recordstore.DBTX, id string) error {
	if err := recordstore.ValidateSaveStates(ctx, tx, id); err != nil {
		return fmt.Errorf("validate session record: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO game_save_versions(save_state_id) VALUES(?)`, id); err != nil {
		return fmt.Errorf("initialize save data version: %w", err)
	}
	return nil
}

func revokePreviewCapability(ctx context.Context, tx recordstore.DBTX, id string) error {
	if _, err := recordstore.UpdateIsolatedRuntimeCapabilities(ctx, tx, recordstore.Update{
		Set: `
revoked_at_ms=(
SELECT finished_at_ms FROM review_preview_sessions WHERE id=?)
`,
		Scope: recordstore.Scope{
			Where: `
preview_id=? AND revoked_at_ms IS NULL AND EXISTS(
SELECT 1 FROM review_preview_sessions preview WHERE preview.id=preview_id
AND preview.state IN ('FINISHED','EXPIRED','REVOKED'))
`,
			Args: []any{id},
		},
		Values: []any{id},
	}); err != nil {
		return fmt.Errorf("revoke preview capability: %w", err)
	}
	return nil
}
