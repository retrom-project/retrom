package gamecontent

import (
	"context"
	"database/sql"
	"fmt"
)

type replacementQueryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type replacementBinding struct {
	contentID, instanceID, platformID, coreID, artifactID, routeKey, compatibilityJSON string
	variantRevisionID, rpgGeneration, rpgAdapterID, rpgAdapterABI                      string
	rpgArtifactSetSHA256, rpgDependencySHA256, rpgRequirementsSHA256                   string
	dependencySnapshotJSON                                                             string
	version, platformVersion, artifactVersion                                          int64
	datID                                                                              sql.NullString
}

func loadReplacementBinding(
	ctx context.Context,
	database replacementQueryRower,
	gameID string,
) (replacementBinding, error) {
	var binding replacementBinding
	var defaultCoreID string
	err := database.QueryRowContext(ctx, `
SELECT game.current_content_revision_id,game.platform_instance_id,instance.platform_id,
       instance.default_core_id,game.version,instance.version
FROM games game
JOIN platform_instances instance ON instance.id=game.platform_instance_id
WHERE game.id=? AND game.status='PUBLISHED'
`, gameID).Scan(
		&binding.contentID, &binding.instanceID, &binding.platformID,
		&defaultCoreID, &binding.version, &binding.platformVersion,
	)
	if err != nil {
		return replacementBinding{}, fmt.Errorf("load replacement game: %w", err)
	}
	if binding.platformID == "rpgmaker" {
		return loadRPGMakerReplacementBinding(ctx, database, gameID, binding)
	}
	err = database.QueryRowContext(ctx, `
SELECT artifact.core_id,artifact.id,artifact.route_key,artifact.version,artifact.compatibility_json,
       (SELECT id FROM dat_versions WHERE core_artifact_id=artifact.id AND is_active=1),
       COALESCE(variant.current_revision_id,'')
FROM core_artifacts artifact
LEFT JOIN game_variants variant ON variant.game_id=? AND variant.core_id=artifact.core_id
WHERE artifact.core_id=? AND artifact.selected_for_new_bindings=1 AND artifact.available_for_launch=1
`, gameID, defaultCoreID).Scan(
		&binding.coreID, &binding.artifactID, &binding.routeKey, &binding.artifactVersion,
		&binding.compatibilityJSON, &binding.datID, &binding.variantRevisionID,
	)
	if err != nil {
		return replacementBinding{}, fmt.Errorf("load replacement artifact: %w", err)
	}
	return binding, nil
}

func loadRPGMakerReplacementBinding(
	ctx context.Context,
	database replacementQueryRower,
	gameID string,
	binding replacementBinding,
) (replacementBinding, error) {
	err := database.QueryRowContext(ctx, `
SELECT variant.core_id,revision.id,artifact.id,artifact.route_key,artifact.version,
       artifact.compatibility_json,profile.generation,profile.adapter_id,profile.adapter_abi,
       profile.artifact_set_sha256,profile.dependency_snapshot_sha256,
       content.requirements_sha256,revision.dependency_snapshot_json
FROM game_variants variant
JOIN game_variant_revisions revision ON revision.id=variant.current_revision_id
JOIN core_artifacts artifact ON artifact.id=revision.core_artifact_id
  AND artifact.runtime_family='RPGMAKER' AND artifact.available_for_launch=1
JOIN rpgmaker_variant_profiles profile ON profile.game_variant_revision_id=revision.id
JOIN rpgmaker_content_profiles content ON content.content_revision_id=revision.game_content_revision_id
WHERE variant.game_id=? AND revision.game_content_revision_id=?
`, gameID, binding.contentID).Scan(
		&binding.coreID, &binding.variantRevisionID, &binding.artifactID, &binding.routeKey,
		&binding.artifactVersion, &binding.compatibilityJSON, &binding.rpgGeneration,
		&binding.rpgAdapterID, &binding.rpgAdapterABI, &binding.rpgArtifactSetSHA256,
		&binding.rpgDependencySHA256, &binding.rpgRequirementsSHA256,
		&binding.dependencySnapshotJSON,
	)
	if err != nil {
		return replacementBinding{}, fmt.Errorf("load RPG Maker replacement binding: %w", err)
	}
	return binding, nil
}

func replacementBindingMatchesSnapshot(binding replacementBinding, snapshot jobSnapshot) bool {
	return binding.identity() == snapshot.bindingIdentity()
}

type replacementBindingIdentity struct {
	contentID, instanceID, platformID, coreID, artifactID, routeKey string
	variantRevisionID, generation, adapterID, adapterABI            string
	artifactSetSHA256, dependencySHA256, requirementsSHA256         string
	version, platformVersion, artifactVersion                       int64
}

func (binding replacementBinding) identity() replacementBindingIdentity {
	return replacementBindingIdentity{
		contentID: binding.contentID, instanceID: binding.instanceID, platformID: binding.platformID,
		coreID: binding.coreID, artifactID: binding.artifactID, routeKey: binding.routeKey,
		variantRevisionID: binding.variantRevisionID, generation: binding.rpgGeneration,
		adapterID: binding.rpgAdapterID, adapterABI: binding.rpgAdapterABI,
		artifactSetSHA256: binding.rpgArtifactSetSHA256,
		dependencySHA256:  binding.rpgDependencySHA256, requirementsSHA256: binding.rpgRequirementsSHA256,
		version: binding.version, platformVersion: binding.platformVersion,
		artifactVersion: binding.artifactVersion,
	}
}

func (snapshot jobSnapshot) bindingIdentity() replacementBindingIdentity {
	return replacementBindingIdentity{
		contentID: snapshot.BaseContentRevisionID, instanceID: snapshot.PlatformInstanceID,
		platformID: snapshot.PlatformID, coreID: snapshot.CoreID, artifactID: snapshot.CoreArtifactID,
		routeKey: snapshot.CoreArtifactRouteKey, variantRevisionID: snapshot.BaseVariantRevisionID,
		generation: snapshot.RPGGeneration, adapterID: snapshot.RPGAdapterID,
		adapterABI: snapshot.RPGAdapterABI, artifactSetSHA256: snapshot.RPGArtifactSetSHA256,
		dependencySHA256:   snapshot.RPGDependencySHA256,
		requirementsSHA256: snapshot.RPGRequirementsSHA256,
		version:            snapshot.GameVersion, platformVersion: snapshot.PlatformInstanceVersion,
		artifactVersion: snapshot.CoreArtifactVersion,
	}
}
