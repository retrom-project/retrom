package gamecontent

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/contentcapability"
)

type replacementQueryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type replacementBinding struct {
	manifestDigest, instanceID, platformID, coreID string
	providerID, targetID                           string
	contentPolicy                                  contentcapability.Policy
	variantID, rpgGeneration, rpgDependencySHA256  string
	rpgRequirementsSHA256, dependencySnapshotJSON  string
	version, platformVersion                       int64
	datID                                          sql.NullString
}

func loadReplacementBinding(
	ctx context.Context,
	database replacementQueryRower,
	gameID string,
) (replacementBinding, error) {
	var binding replacementBinding
	var defaultCoreID string
	err := database.QueryRowContext(ctx, `
SELECT game.source_manifest_digest,game.platform_instance_id,instance.platform_id,
       instance.default_core_id,game.version,instance.version
FROM games game
JOIN platform_instances instance ON instance.id=game.platform_instance_id
WHERE game.id=? AND game.status='PUBLISHED'
`, gameID).Scan(
		&binding.manifestDigest, &binding.instanceID, &binding.platformID,
		&defaultCoreID, &binding.version, &binding.platformVersion,
	)
	if err != nil {
		return replacementBinding{}, fmt.Errorf("load replacement game: %w", err)
	}
	if binding.platformID == "rpgmaker" {
		return loadRPGMakerReplacementBinding(ctx, database, gameID, binding)
	}
	err = database.QueryRowContext(ctx, `
SELECT binding.core_id,target.provider_id,target.target_id,`+contentcapability.BindingPolicySQL+`,
       (SELECT id FROM dat_versions dat
        WHERE dat.provider_id=target.provider_id AND dat.target_id=target.target_id AND dat.is_active=1),
       COALESCE(variant.id,'')
FROM runtime_target_bindings binding
JOIN runtime_binding_platforms platform_binding
  ON platform_binding.binding_id=binding.binding_id AND platform_binding.platform_id=?
JOIN runtime_targets target
  ON target.provider_id=binding.provider_id AND target.target_id=binding.target_id
LEFT JOIN game_variants variant ON variant.game_id=? AND variant.core_id=binding.core_id
WHERE binding.core_id=? AND binding.launch_policy<>'DISABLED'
`, binding.platformID, gameID, defaultCoreID).Scan(
		&binding.coreID, &binding.providerID, &binding.targetID,
		&binding.contentPolicy, &binding.datID, &binding.variantID,
	)
	if err != nil {
		return replacementBinding{}, fmt.Errorf("load replacement target: %w", err)
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
SELECT variant.core_id,variant.id,target.provider_id,target.target_id,`+contentcapability.BindingPolicySQL+`,
       profile.generation,profile.dependency_snapshot_sha256,content.requirements_sha256,
       variant.dependency_snapshot_json
FROM game_variants variant
JOIN runtime_targets target
  ON target.provider_id=variant.provider_id AND target.target_id=variant.target_id
JOIN runtime_target_bindings binding
  ON binding.provider_id=target.provider_id AND binding.target_id=target.target_id
  AND binding.core_id=variant.core_id AND binding.launch_policy<>'DISABLED'
JOIN rpgmaker_variant_profiles profile ON profile.game_variant_id=variant.id
JOIN rpgmaker_game_profiles content ON content.game_id=variant.game_id
WHERE variant.game_id=?
	`, gameID).Scan(
		&binding.coreID, &binding.variantID, &binding.providerID, &binding.targetID,
		&binding.contentPolicy,
		&binding.rpgGeneration, &binding.rpgDependencySHA256, &binding.rpgRequirementsSHA256,
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
	manifestDigest, instanceID, platformID, coreID string
	providerID, targetID                           string
	contentPolicyDigest                            string
	variantID, generation, dependencySHA256        string
	requirementsSHA256                             string
	version, platformVersion                       int64
}

func (binding replacementBinding) identity() replacementBindingIdentity {
	return replacementBindingIdentity{
		manifestDigest: binding.manifestDigest, instanceID: binding.instanceID, platformID: binding.platformID,
		coreID: binding.coreID, providerID: binding.providerID, targetID: binding.targetID,
		contentPolicyDigest: binding.contentPolicy.Digest(),
		variantID:           binding.variantID, generation: binding.rpgGeneration,
		dependencySHA256: binding.rpgDependencySHA256, requirementsSHA256: binding.rpgRequirementsSHA256,
		version: binding.version, platformVersion: binding.platformVersion,
	}
}

func (snapshot jobSnapshot) bindingIdentity() replacementBindingIdentity {
	return replacementBindingIdentity{
		manifestDigest: snapshot.BaseManifestDigest, instanceID: snapshot.PlatformInstanceID,
		platformID: snapshot.PlatformID, coreID: snapshot.CoreID,
		providerID: snapshot.ProviderID, targetID: snapshot.TargetID,
		contentPolicyDigest: snapshot.ContentPolicy.Digest(),
		variantID:           snapshot.VariantID, generation: snapshot.RPGGeneration,
		dependencySHA256:   snapshot.RPGDependencySHA256,
		requirementsSHA256: snapshot.RPGRequirementsSHA256,
		version:            snapshot.GameVersion, platformVersion: snapshot.PlatformInstanceVersion,
	}
}
