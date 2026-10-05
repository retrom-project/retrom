package saves

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/persistence/filedeletion"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/service/saves"
)

func (store records) Binding(ctx context.Context, id string) (saves.GameSaveBinding, bool, error) {
	var binding saves.GameSaveBinding
	err := dbapi.QueryRowContext(ctx, store.executor, `SELECT save_state_id,expected_data_version
FROM launch_game_save_bindings WHERE launch_session_id=?`, id).Scan(&binding.ID, &binding.ExpectedVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return saves.GameSaveBinding{}, false, nil
	}
	if err != nil {
		return saves.GameSaveBinding{}, false, fmt.Errorf("read checkpoint binding: %w", err)
	}
	return binding, true, nil
}

func (store records) Saved(ctx context.Context, id string) (saves.StoredSave, bool, error) {
	var saved saves.StoredSave
	err := dbapi.QueryRowContext(ctx, store.executor, `SELECT save.id,save.name,save.created_at_ms,save.version,
 save.active_duration_ms,save.payload_sha256,native.data_version,save.screenshot_file_record,
 save.profile_id,save.game_id,save.checkpoint_format,save.deleted_at_ms
FROM save_states save JOIN game_save_versions native ON native.save_state_id=save.id WHERE
save.id=?`, id).
		Scan(&saved.Result.SaveStateID, &saved.Result.Name, &saved.Result.CreatedAtMS, &saved.Result.Version,
			&saved.Result.ActiveDurationMS, &saved.Digest, &saved.DataVersion, &saved.ScreenshotID, &saved.ProfileID,
			&saved.GameID, &saved.Format, &saved.DeletedAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return saves.StoredSave{}, false, nil
	}
	if err != nil {
		return saves.StoredSave{}, false, fmt.Errorf("read saved slot: %w", err)
	}
	return saved, true, nil
}

func (store records) MarkSynced(ctx context.Context, saveID, launchID string, now int64) error {
	return changed(recordstore.UpdateGameSaveVersions(ctx, store.executor, recordstore.Update{
		Set:   `last_synced_at_ms=?,last_writer_launch_session_id=?`,
		Scope: recordstore.Scope{Where: `save_state_id=?`, Args: []any{saveID}}, Values: []any{now, launchID},
	}))
}

func (store records) Bind(ctx context.Context, launchID, saveID string, version int64) error {
	return changed(store.executor.ExecContext(ctx, `UPDATE launch_game_save_bindings
SET save_state_id=?,expected_data_version=? WHERE launch_session_id=?`, saveID, version, launchID))
}

func (store records) UpdateSave(ctx context.Context, update saves.SaveUpdate) error {
	old, err := dbapi.QueryStrings(ctx, store.executor, `
SELECT payload_file_record FROM save_states WHERE id=$1 AND payload_file_record IS NOT NULL
 UNION SELECT screenshot_file_record FROM save_states WHERE id=$1 AND screenshot_file_record IS NOT NULL
`, update.SaveID)
	if err != nil {
		return fmt.Errorf("update save: %w", err)
	}
	for _, value := range old {
		if value == update.PayloadID || value == update.ScreenshotID {
			continue
		}
		if err := filedeletion.QueueFile(ctx, store.executor, value, update.AtMS); err != nil {
			return fmt.Errorf("update save: %w", err)
		}
	}

	if err := changed(recordstore.UpdateGameSaveVersions(ctx, store.executor, recordstore.Update{
		Set: `last_synced_at_ms=?,last_writer_launch_session_id=?,data_version=data_version+1`,
		Scope: recordstore.Scope{
			Where: `save_state_id=? AND data_version=?`,
			Args: []any{
				update.SaveID,
				update.ExpectedDataVersion,
			},
		},
		Values: []any{update.AtMS, update.LaunchID},
	})); err != nil {
		return err
	}
	return changed(recordstore.UpdateSaveStates(ctx, store.executor, recordstore.Update{
		Set: `payload_file_record=?,payload_sha256=?,payload_size_bytes=?,screenshot_file_record=?,
updated_at_ms=?,active_duration_ms=?,version=version+1`,
		Scope: recordstore.Scope{Where: `id=? AND deleted_at_ms IS NULL`, Args: []any{update.SaveID}},
		Values: []any{
			update.PayloadID, update.Payload.SHA256, update.Payload.Size, update.ScreenshotID,
			update.AtMS, update.ActiveDurationMS,
		},
	}))
}
