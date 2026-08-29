package launch

import (
	"context"
	"database/sql"
)

type projectProductConfigSource struct {
	credentialHash                                                    []byte
	state, coreID, coreName, artifactID, runtimeVersion, adapterKind  string
	adapterID, compatibilityJSON, dependencyJSON, gameTitle, platform string
	returnTo                                                          string
	bootstrapExpires, hardExpires                                     int64
	idleExpires                                                       sql.NullInt64
	saveStateID                                                       sql.NullString
}

func (service *Service) loadProjectProductConfigSource(
	ctx context.Context,
	launchID, runtimeFamily string,
) (projectProductConfigSource, error) {
	var source projectProductConfigSource
	err := service.database.QueryRowContext(ctx, `
SELECT launch.credential_sha256,launch.state,launch.bootstrap_expires_at_ms,
 launch.hard_expires_at_ms,launch.idle_expires_at_ms,artifact.core_id,core.name,
 artifact.id,artifact.runtime_version,artifact.runtime_adapter_kind,artifact.adapter_id,
 artifact.compatibility_json,revision.dependency_snapshot_json,metadata.title,platform.name,
 launch.return_to,launch.save_state_id
FROM launch_sessions launch
JOIN core_artifacts artifact ON artifact.id=launch.core_artifact_id
JOIN cores core ON core.id=artifact.core_id
JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
JOIN core_artifacts bound_artifact ON bound_artifact.id=revision.core_artifact_id
JOIN games game ON game.id=launch.game_id
JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
WHERE launch.id=? AND launch.purpose='PRODUCT' AND artifact.runtime_family=?
 AND artifact.available_for_launch=1 AND revision.status='READY'
 AND bound_artifact.core_id=artifact.core_id AND bound_artifact.route_key=artifact.route_key
 AND json_extract(bound_artifact.compatibility_json,'$.gameCompatibilityLine')=
     json_extract(artifact.compatibility_json,'$.gameCompatibilityLine')
`, launchID, runtimeFamily).Scan(
		&source.credentialHash, &source.state, &source.bootstrapExpires, &source.hardExpires,
		&source.idleExpires, &source.coreID, &source.coreName, &source.artifactID,
		&source.runtimeVersion, &source.adapterKind, &source.adapterID, &source.compatibilityJSON,
		&source.dependencyJSON, &source.gameTitle, &source.platform, &source.returnTo, &source.saveStateID,
	)
	if err != nil {
		return projectProductConfigSource{}, ErrCredential
	}
	return source, nil
}
