package saves

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/model/saves"
)

func (store records) LoadLaunch(ctx context.Context, id string) (saves.Launch, error) {
	var result saves.Launch
	var checkpoint []byte
	err := store.executor.QueryRowContext(ctx, `
SELECT COALESCE(user.id,launch.profile_id),launch.profile_id,'PRODUCT',launch.game_id,
 launch.provider_id,launch.target_id,launch.dos_entry_path,launch.credential_sha256,launch.state,
 launch.hard_expires_at_ms,target.checkpoint_json,
 CASE WHEN EXISTS(SELECT 1 FROM launch_content_files file
  WHERE file.launch_session_id=launch.id AND file.format_version='RETROM_MULTIDISC_M3U_V1')
 THEN 'RETROM_MULTIDISC_M3U_V1' ELSE COALESCE((SELECT min(file.format_version)
  FROM launch_content_files file WHERE file.launch_session_id=launch.id),'') END,
 (SELECT count(*) FROM launch_external_files external
  WHERE external.launch_session_id=launch.id AND external.kind='DISC'),launch.initial_disc_index,
 game.status,'','',EXISTS(SELECT 1 FROM launch_game_save_bindings binding WHERE binding.launch_session_id=launch.id)
FROM launch_sessions launch JOIN games game ON game.id=launch.game_id
JOIN runtime_targets target ON target.provider_id=launch.provider_id AND target.target_id=launch.target_id
LEFT JOIN users user ON user.profile_id=launch.profile_id WHERE launch.id=?
UNION ALL
SELECT actor.id,actor.profile_id,'REVIEW_PREVIEW','',
 preview.provider_id,preview.target_id,preview.default_dos_entry,preview.credential_sha256,preview.state,
 preview.hard_expires_at_ms,target.checkpoint_json,preview.content_format,
 (SELECT count(*) FROM review_preview_files file WHERE file.preview_session_id=preview.id AND file.role='DISC'),
 0,'',item.state,item.payload_state,0
FROM review_preview_sessions preview JOIN users actor ON actor.id=preview.actor_user_id
JOIN import_items item ON item.id=preview.import_item_id
JOIN runtime_targets target ON target.provider_id=preview.provider_id AND target.target_id=preview.target_id
WHERE preview.id=?`, id, id).Scan(&result.PrincipalID, &result.ProfileID, &result.Purpose, &result.GameID,
		&result.ProviderID, &result.TargetID, &result.DOSEntry, &result.CredentialHash,
		&result.State, &result.HardExpiresAtMS, &checkpoint, &result.ContentFormat,
		&result.DiscCount, &result.InitialDiscIndex, &result.GameStatus, &result.ItemState,
		&result.PayloadState, &result.HasGameSaveBinding)
	if errors.Is(err, sql.ErrNoRows) {
		return saves.Launch{}, saves.ErrCredential
	}
	if err != nil {
		return saves.Launch{}, fmt.Errorf("query checkpoint launch: %w", err)
	}
	if len(checkpoint) > 0 {
		if err := json.Unmarshal(checkpoint, &result.Checkpoint); err != nil {
			return saves.Launch{}, fmt.Errorf("decode launch checkpoint declaration: %w", err)
		}
	}
	return result, nil
}

func (store records) Restore(ctx context.Context, id string) (saves.Restore, error) {
	var result saves.Restore
	var checkpoint []byte
	err := store.executor.QueryRowContext(ctx, `
SELECT target.checkpoint_json,blob.sha256,blob.size_bytes,save.checkpoint_format
FROM launch_sessions launch
JOIN runtime_targets target ON target.provider_id=launch.provider_id AND target.target_id=launch.target_id
JOIN save_states save ON save.id=launch.save_state_id AND save.deleted_at_ms IS NULL
 AND save.profile_id=launch.profile_id AND save.game_id=launch.game_id
LEFT JOIN launch_game_save_bindings binding ON binding.launch_session_id=launch.id
JOIN blobs blob ON blob.id=COALESCE(binding.restore_payload_blob_id,save.payload_blob_id)
 AND (binding.restore_payload_blob_id IS NOT NULL OR
 (blob.sha256=save.payload_sha256 AND blob.size_bytes=save.payload_size_bytes))
WHERE launch.id=?
UNION ALL
SELECT target.checkpoint_json,blob.sha256,blob.size_bytes,preview.restore_checkpoint_format
FROM review_preview_sessions preview
JOIN runtime_targets target ON target.provider_id=preview.provider_id AND target.target_id=preview.target_id
JOIN blobs blob ON blob.id=preview.restore_payload_blob_id WHERE preview.id=?`, id, id).
		Scan(&checkpoint, &result.Digest, &result.Size, &result.Format)
	if errors.Is(err, sql.ErrNoRows) {
		return saves.Restore{}, saves.ErrCheckpointIncompatible
	}
	if err != nil {
		return saves.Restore{}, fmt.Errorf("query checkpoint restore: %w", err)
	}
	if len(checkpoint) > 0 {
		if err := json.Unmarshal(checkpoint, &result.Checkpoint); err != nil {
			return saves.Restore{}, fmt.Errorf("decode restore checkpoint declaration: %w", err)
		}
	}
	return result, nil
}
