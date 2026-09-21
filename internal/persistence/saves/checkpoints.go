package saves

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/sessionstore"
	"retrom/internal/service/saves"
)

func (store records) Duration(ctx context.Context, id string) (saves.Duration, error) {
	var duration saves.Duration
	err := store.executor.QueryRowContext(ctx, `SELECT
 COALESCE((SELECT active_duration_ms FROM play_sessions WHERE launch_session_id=?),0),
 COALESCE((SELECT initial_active_duration_ms FROM launch_game_save_bindings WHERE launch_session_id=?),0)`, id, id).
		Scan(&duration.ActiveMS, &duration.InitialMS)
	if err != nil {
		return saves.Duration{}, fmt.Errorf("read checkpoint duration: %w", err)
	}
	return duration, nil
}

func (store writes) CreateSave(ctx context.Context, creation saves.SaveCreation) error {
	result := creation.Result
	if _, err := sessionstore.CreateSave(ctx, store.transaction, `
INSERT INTO save_states(id,profile_id,game_id,checkpoint_format,dos_entry_path,payload_blob_id,payload_sha256,
 payload_size_bytes,screenshot_blob_id,name,active_duration_ms,version,created_at_ms,updated_at_ms,
 source_launch_session_id,disc_index) VALUES(?,?,?,?,?,?,?,?,?,?,?,1,?,?,?,?)`,
		result.SaveStateID, creation.ProfileID, creation.GameID, result.CheckpointFormat,
		creation.DOSEntry, creation.PayloadID,
		creation.Payload.SHA256, creation.Payload.Size, creation.ScreenshotID, result.Name, result.ActiveDurationMS,
		result.CreatedAtMS, result.CreatedAtMS, creation.LaunchID, result.DiscIndex); err != nil {
		return fmt.Errorf("insert checkpoint: %w", err)
	}
	return nil
}

func (store writes) ReplacePreview(ctx context.Context, update saves.PreviewWrite) error {
	return changed(sessionstore.ChangePreview(ctx, store.transaction, recordstore.Update{
		Set: `checkpoint_payload_blob_id=?,checkpoint_format=?,checkpoint_created_at_ms=?,
updated_at_ms=?,version=version+1`,
		Scope:  recordstore.Scope{Where: `id=?`, Args: []any{update.PreviewID}},
		Values: []any{update.PayloadID, update.Format, update.AtMS, update.AtMS},
	}))
}
