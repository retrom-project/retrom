package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/launch"
)

func (repository *ContentQueries) External(
	ctx context.Context,
	ref application.SessionRef,
	logicalName string,
) (application.ExternalRecord, bool, error) {
	query := `
SELECT launch.credential_sha256,
launch.state,
launch.hard_expires_at_ms,
blob.value,json_extract(blob.value, '$.sha256'),
file.kind,
platform.id,
launch.core_id,
launch.provider_id,launch.target_id,launch.bundle_sha256,
(SELECT count(*) FROM launch_external_files disc
 WHERE disc.launch_session_id=launch.id AND disc.kind='DISC')
FROM launch_sessions launch
JOIN launch_external_files file ON file.launch_session_id=launch.id
JOIN json_each(json_array(file.file_record)) blob ON blob.value IS NOT NULL
JOIN games game ON game.id=launch.game_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
WHERE launch.id=?
AND file.logical_name=?
`
	if ref.Preview {
		query = `
SELECT preview.credential_sha256,preview.state,preview.hard_expires_at_ms,blob.value,
json_extract(blob.value, '$.sha256'),
CASE WHEN file.role='DISC' THEN 'DISC' ELSE 'BIOS' END,
platform.id,binding.core_id,preview.provider_id,preview.target_id,preview.bundle_sha256,
(SELECT count(*) FROM runtime_preview_files disc WHERE disc.preview_session_id=preview.id AND disc.role='DISC')
FROM runtime_preview_sessions preview
JOIN runtime_preview_files file ON file.preview_session_id=preview.id AND file.role IN ('EXTERNAL_FILE','DISC')
JOIN json_each(json_array(file.file_record)) blob ON blob.value IS NOT NULL
JOIN runtime_target_bindings binding ON binding.provider_id=preview.provider_id AND
binding.target_id=preview.target_id
JOIN platform_instances instance ON instance.id=preview.target_platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
WHERE preview.id=? AND file.logical_name=?
`
	}
	var result application.ExternalRecord
	session, content := &result.Session, &result.Content
	err := dbapi.QueryRowContext(ctx, repository.executor, query, ref.ID, logicalName).Scan(
		&session.CredentialHash, &session.State, &session.HardExpiresAtMS,
		&content.FileRecord, &content.Digest, &content.Kind, &content.PlatformKey, &content.CoreKey,
		&content.ProviderID, &content.TargetID, &content.BundleSHA256, &content.DiscCount)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ExternalRecord{}, false, nil
	}
	if err != nil {
		return application.ExternalRecord{}, false, fmt.Errorf("scan launch external: %w", err)
	}
	return result, true, nil
}
