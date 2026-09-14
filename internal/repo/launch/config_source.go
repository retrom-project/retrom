package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	application "retrom/internal/model/launch"
	"retrom/internal/repo/dbexec"
)

const productConfigSQL = `
SELECT launch.credential_sha256,launch.state,launch.version,launch.provider_id,launch.target_id,
 launch.bundle_sha256,
 launch.core_id,core.name,binding.detector_profile,binding.delivery_profile,
 'PRODUCT',game.title,platform.name,launch.return_to,
 launch.content_kind,launch.dependency_snapshot_json,launch.compatibility_code,
 launch.save_state_id,launch.dos_entry_path,
 launch.netplay_session_id,launch.netplay_player_no,session.room_id,session.profile_json,
 launch.bootstrap_expires_at_ms,launch.hard_expires_at_ms,launch.idle_expires_at_ms,
 launch.initial_disc_index
FROM launch_sessions launch
JOIN cores core ON core.id=launch.core_id
JOIN games game ON game.id=launch.game_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
JOIN runtime_target_bindings binding ON binding.provider_id=launch.provider_id AND binding.target_id=launch.target_id
LEFT JOIN netplay_sessions session ON session.id=launch.netplay_session_id
WHERE launch.id=?
`

const previewConfigSQL = `
SELECT preview.credential_sha256,preview.state,preview.version,preview.provider_id,preview.target_id,
	 preview.bundle_sha256,
 binding.core_id,core.name,binding.detector_profile,binding.delivery_profile,
 'REVIEW_PREVIEW',preview.title,instance.name,
 '/admin/reviews/' || preview.import_item_id,preview.content_kind,preview.dependency_snapshot_json,'',
	 NULL,preview.default_dos_entry,NULL,NULL,NULL,NULL,
 preview.bootstrap_expires_at_ms,preview.hard_expires_at_ms,NULL,0
FROM review_preview_sessions preview
JOIN platform_instances instance ON instance.id=preview.target_platform_instance_id
JOIN runtime_target_bindings binding ON binding.provider_id=preview.provider_id AND binding.target_id=preview.target_id
JOIN cores core ON core.id=binding.core_id
WHERE preview.id=?
`

func configSource(
	ctx context.Context,
	executor dbexec.Executor,
	ref application.SessionRef,
) (application.ConfigSource, bool, error) {
	query := productConfigSQL
	if ref.Preview {
		query = previewConfigSQL
	}
	return scanConfigSource(executor.QueryRowContext(ctx, query, ref.ID))
}

func scanConfigSource(row dbexec.Scanner) (application.ConfigSource, bool, error) {
	var source application.ConfigSource
	err := row.Scan(
		&source.CredentialHash, &source.State, &source.Version, &source.ProviderID, &source.TargetID,
		&source.BundleDigest, &source.CoreID, &source.CoreName,
		&source.DetectorProfile, &source.Delivery, &source.Purpose, &source.Title, &source.PlatformName, &source.ReturnTo,
		&source.ContentKind, &source.DependencyJSON, &source.Compatibility, &source.SaveID,
		&source.DOSEntry, &source.NetplayID, &source.NetplayPlayer,
		&source.NetplayRoom, &source.NetplayProfile, &source.BootstrapEnd, &source.HardEnd,
		&source.IdleEnd, &source.InitialDisc,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ConfigSource{}, false, nil
	}
	if err != nil {
		return application.ConfigSource{}, false, fmt.Errorf("read config source: %w", err)
	}
	return source, true, nil
}
