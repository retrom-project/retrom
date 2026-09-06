package saves

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/blobstore"
)

type gameSaveBinding struct {
	id       sql.NullString
	expected int64
}

func (service *Service) persistProductCheckpoint(
	ctx context.Context, tx *sql.Tx, launchID string, launch launchSnapshot,
	parsed parsedManual, payloadID string, now int64,
) (ManualResult, error) {
	var binding gameSaveBinding
	err := tx.QueryRowContext(ctx, `SELECT save_state_id,expected_data_version
FROM launch_game_save_bindings WHERE launch_session_id=?`, launchID).Scan(&binding.id, &binding.expected)
	if errors.Is(err, sql.ErrNoRows) {
		return service.insertProductSave(ctx, tx, launchID, launch, parsed, payloadID, now)
	}
	if err != nil {
		return ManualResult{}, fmt.Errorf("load native save binding: %w", err)
	}
	if parsed.screenshot == nil {
		return ManualResult{}, ErrCheckpointInvalid
	}
	var result ManualResult
	if !binding.id.Valid {
		if binding.expected != 0 {
			return ManualResult{}, ErrSyncConflict
		}
		result, err = service.insertProductSave(ctx, tx, launchID, launch, parsed, payloadID, now)
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE save_states SET last_synced_at_ms=?,last_writer_launch_session_id=?
WHERE id=?`, now, launchID, result.SaveStateID)
		}
	} else {
		result, err = service.updateGameSave(ctx, tx, launchID, launch, parsed, payloadID, binding, now)
	}
	if err != nil {
		return ManualResult{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE launch_game_save_bindings
SET save_state_id=?,expected_data_version=(SELECT data_version FROM save_states WHERE id=?)
WHERE launch_session_id=?`, result.SaveStateID, result.SaveStateID, launchID)
	if err != nil {
		return ManualResult{}, fmt.Errorf("bind native save: %w", err)
	}
	return result, nil
}

func (service *Service) updateGameSave(
	ctx context.Context, tx *sql.Tx, launchID string, launch launchSnapshot, parsed parsedManual,
	payloadID string, binding gameSaveBinding, now int64,
) (ManualResult, error) {
	var result ManualResult
	var digest string
	var dataVersion int64
	var screenshotID sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT id,name,created_at_ms,version,active_duration_ms,
payload_sha256,data_version,screenshot_blob_id FROM save_states
WHERE id=? AND profile_id=? AND game_id=? AND checkpoint_format=? AND deleted_at_ms IS NULL`,
		binding.id.String, launch.profileID, launch.gameID, launch.checkpointFormat).Scan(
		&result.SaveStateID, &result.Name, &result.CreatedAtMS, &result.Version, &result.ActiveDurationMS,
		&digest, &dataVersion, &screenshotID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && dataVersion != binding.expected {
		return ManualResult{}, ErrSyncConflict
	}
	if err != nil {
		return ManualResult{}, fmt.Errorf("read native save slot: %w", err)
	}
	result.ResourceKind = "SAVE_STATE"
	result.CheckpointFormat = launch.checkpointFormat
	if screenshotID.Valid {
		value := "/content/save-states/" + result.SaveStateID + "/screenshot"
		result.ScreenshotURL = &value
	}
	if digest == parsed.payload.SHA256 {
		return result, nil
	}
	imageID, err := blobstore.EnsureRecord(ctx, tx, *parsed.screenshot, parsed.screenshotMediaType, now)
	if err != nil {
		return ManualResult{}, fmt.Errorf("store native save image: %w", err)
	}
	updated, err := tx.ExecContext(ctx, `UPDATE save_states SET payload_blob_id=?,payload_sha256=?,payload_size_bytes=?,
screenshot_blob_id=?,updated_at_ms=?,last_synced_at_ms=?,last_writer_launch_session_id=?,
active_duration_ms=(SELECT binding.initial_active_duration_ms+COALESCE(play.active_duration_ms,0)
 FROM launch_game_save_bindings binding LEFT JOIN play_sessions play ON play.launch_session_id=binding.launch_session_id
 WHERE binding.launch_session_id=?),version=version+1,data_version=data_version+1
WHERE id=? AND data_version=? AND deleted_at_ms IS NULL`,
		payloadID, parsed.payload.SHA256, parsed.payload.Size, imageID, now, now, launchID, launchID,
		result.SaveStateID, binding.expected)
	if err != nil {
		return ManualResult{}, fmt.Errorf("update native save: %w", err)
	}
	affected, err := updated.RowsAffected()
	if err != nil {
		return ManualResult{}, fmt.Errorf("count native save update: %w", err)
	}
	if affected != 1 {
		return ManualResult{}, ErrSyncConflict
	}
	if err := tx.QueryRowContext(ctx, `SELECT active_duration_ms FROM save_states WHERE id=?`, result.SaveStateID).
		Scan(&result.ActiveDurationMS); err != nil {
		return ManualResult{}, fmt.Errorf("read updated native save duration: %w", err)
	}
	result.Version++
	value := "/content/save-states/" + result.SaveStateID + "/screenshot"
	result.ScreenshotURL = &value
	return result, nil
}
