package saves

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	retromruntime "retrom/internal/runtime"
)

const maxStoredCheckpointBytes = int64(256 << 20)

type launchSnapshot struct {
	principalID, profileID, purpose, gameID string
	providerID, targetID, checkpointFormat  string
	dosEntry                                sql.NullString
	credentialHash                          []byte
	state                                   string
	hardExpiresAtMS, checkpointMaxBytes     int64
	contentFormat                           string
	discCount, initialDiscIndex             int
}

func (service *Service) launch(ctx context.Context, launchID, capability string) (launchSnapshot, error) {
	var result launchSnapshot
	var writeFormat sql.NullString
	var checkpointMaxBytes sql.NullInt64
	err := service.database.QueryRowContext(ctx, `
SELECT COALESCE(user.id,launch.profile_id),launch.profile_id,'PRODUCT',launch.game_id,
 launch.provider_id,launch.target_id,launch.dos_entry_path,launch.credential_sha256,launch.state,
 launch.hard_expires_at_ms,json_extract(target.checkpoint_json,'$.writeFormat'),
 json_extract(target.checkpoint_json,'$.maxBytes'),
 CASE WHEN EXISTS(SELECT 1 FROM launch_content_files file
       WHERE file.launch_session_id=launch.id AND file.format_version='RETROM_MULTIDISC_M3U_V1')
      THEN 'RETROM_MULTIDISC_M3U_V1'
      ELSE COALESCE((SELECT min(file.format_version) FROM launch_content_files file
                     WHERE file.launch_session_id=launch.id),'') END,
 (SELECT count(*) FROM launch_external_files external
  WHERE external.launch_session_id=launch.id AND external.kind='DISC'),
 launch.initial_disc_index
FROM launch_sessions launch
JOIN runtime_targets target ON target.provider_id=launch.provider_id AND target.target_id=launch.target_id
LEFT JOIN users user ON user.profile_id=launch.profile_id
WHERE launch.id=? AND launch.game_id IS NOT NULL
UNION ALL
SELECT actor.id,actor.profile_id,'REVIEW_PREVIEW','',
 preview.provider_id,preview.target_id,preview.default_dos_entry,preview.credential_sha256,preview.state,
 preview.hard_expires_at_ms,json_extract(target.checkpoint_json,'$.writeFormat'),
 json_extract(target.checkpoint_json,'$.maxBytes'),preview.content_format,
 (SELECT count(*) FROM review_preview_files file WHERE file.preview_session_id=preview.id AND file.role='DISC'),0
FROM review_preview_sessions preview
JOIN users actor ON actor.id=preview.actor_user_id
JOIN runtime_targets target ON target.provider_id=preview.provider_id AND target.target_id=preview.target_id
WHERE preview.id=?
`, launchID, launchID).Scan(
		&result.principalID, &result.profileID, &result.purpose, &result.gameID,
		&result.providerID, &result.targetID, &result.dosEntry,
		&result.credentialHash, &result.state, &result.hardExpiresAtMS, &writeFormat,
		&checkpointMaxBytes, &result.contentFormat, &result.discCount, &result.initialDiscIndex,
	)
	if !validLaunchAccess(err, capability, result, service.now().UnixMilli()) {
		return launchSnapshot{}, ErrCredential
	}
	if !writeFormat.Valid || !checkpointMaxBytes.Valid || checkpointMaxBytes.Int64 < 1 {
		return launchSnapshot{}, ErrCheckpointUnavailable
	}
	result.checkpointFormat = writeFormat.String
	result.checkpointMaxBytes = min(checkpointMaxBytes.Int64, maxStoredCheckpointBytes)
	if !validLaunchDiscShape(result) {
		return launchSnapshot{}, ErrCredential
	}
	return result, nil
}

func validLaunchAccess(err error, capability string, launch launchSnapshot, now int64) bool {
	return err == nil && retromruntime.MatchesCapability(capability, launch.credentialHash) &&
		launch.state == "ACTIVE" && launch.hardExpiresAtMS > now
}

func validLaunchDiscShape(result launchSnapshot) bool {
	if result.contentFormat != "RETROM_MULTIDISC_M3U_V1" {
		return result.discCount == 0 && result.initialDiscIndex == 0
	}
	return result.discCount >= 2 && result.initialDiscIndex >= 0 && result.initialDiscIndex < result.discCount
}

func loadLaunchForRestore(
	ctx context.Context, database queryRower, launchID string,
) (launchSnapshot, string, int64, error) {
	var result launchSnapshot
	var digest string
	var savedSize, targetMaximum int64
	err := database.QueryRowContext(ctx, `
SELECT launch.profile_id,launch.game_id,launch.provider_id,launch.target_id,
 json_extract(target.checkpoint_json,'$.maxBytes'),blob.sha256,blob.size_bytes,save.checkpoint_format
FROM launch_sessions launch
JOIN runtime_targets target ON target.provider_id=launch.provider_id AND target.target_id=launch.target_id
JOIN save_states save ON save.id=launch.save_state_id AND save.deleted_at_ms IS NULL
 AND save.profile_id=launch.profile_id AND save.game_id=launch.game_id
JOIN blobs blob ON blob.id=save.payload_blob_id
 AND blob.sha256=save.payload_sha256 AND blob.size_bytes=save.payload_size_bytes
WHERE launch.id=? AND EXISTS(
 SELECT 1 FROM json_each(target.checkpoint_json,'$.readFormats') readable
 WHERE readable.type='text' AND readable.value=save.checkpoint_format)
UNION ALL
SELECT actor.profile_id,'',preview.provider_id,preview.target_id,
 json_extract(target.checkpoint_json,'$.maxBytes'),blob.sha256,blob.size_bytes,preview.restore_checkpoint_format
FROM review_preview_sessions preview
JOIN users actor ON actor.id=preview.actor_user_id
JOIN runtime_targets target ON target.provider_id=preview.provider_id AND target.target_id=preview.target_id
JOIN blobs blob ON blob.id=preview.restore_payload_blob_id
WHERE preview.id=? AND EXISTS(
 SELECT 1 FROM json_each(target.checkpoint_json,'$.readFormats') readable
 WHERE readable.type='text' AND readable.value=preview.restore_checkpoint_format)
`, launchID, launchID).Scan(
		&result.profileID, &result.gameID, &result.providerID, &result.targetID,
		&targetMaximum, &digest, &savedSize, &result.checkpointFormat,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return launchSnapshot{}, "", 0, ErrCheckpointIncompatible
	}
	if err != nil {
		return launchSnapshot{}, "", 0, fmt.Errorf("load restore launch: %w", err)
	}
	result.checkpointMaxBytes = min(targetMaximum, maxStoredCheckpointBytes)
	if savedSize < 1 || savedSize > result.checkpointMaxBytes {
		return launchSnapshot{}, "", 0, ErrCheckpointIncompatible
	}
	return result, digest, savedSize, nil
}
