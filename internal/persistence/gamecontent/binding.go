package gamecontent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/contentcapability"
	"retrom/internal/dbexec"
	"retrom/internal/service/gamecontent"
)

func loadReplacementBinding(
	ctx context.Context,
	database dbexec.Executor,
	gameID string,
) (gamecontent.Binding, error) {
	var binding gamecontent.Binding
	var defaultCoreID string
	err := database.QueryRowContext(ctx, `
SELECT game.source_manifest_digest,game.platform_instance_id,instance.platform_id,
       instance.default_core_id,game.version,instance.version
FROM games game
JOIN platform_instances instance ON instance.id=game.platform_instance_id
WHERE game.id=? AND game.status='PUBLISHED'
`, gameID).Scan(
		&binding.ManifestDigest, &binding.InstanceID, &binding.PlatformID,
		&defaultCoreID, &binding.Version, &binding.PlatformVersion,
	)
	if err != nil {
		return gamecontent.Binding{}, fmt.Errorf("load replacement game: %w", err)
	}
	if binding.PlatformID == "rpgmaker" {
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
`, binding.PlatformID, gameID, defaultCoreID).Scan(
		&binding.CoreID, &binding.ProviderID, &binding.TargetID,
		&binding.ContentPolicy, &binding.DATID, &binding.VariantID,
	)
	if err != nil {
		return gamecontent.Binding{}, fmt.Errorf("load replacement target: %w", err)
	}
	return binding, nil
}

func loadRPGMakerReplacementBinding(
	ctx context.Context,
	database dbexec.Executor,
	gameID string,
	binding gamecontent.Binding,
) (gamecontent.Binding, error) {
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
		&binding.CoreID, &binding.VariantID, &binding.ProviderID, &binding.TargetID,
		&binding.ContentPolicy,
		&binding.RPGGeneration, &binding.RPGDependencySHA256, &binding.RPGRequirementsSHA256,
		&binding.DependencySnapshotJSON,
	)
	if err != nil {
		return gamecontent.Binding{}, fmt.Errorf("load RPG Maker replacement binding: %w", err)
	}
	return binding, nil
}

func (records records) Binding(ctx context.Context, id string) (gamecontent.Binding, error) {
	value, err := loadReplacementBinding(ctx, records.executor, id)
	if errors.Is(err, sql.ErrNoRows) {
		return value, gamecontent.ErrInvalid
	}
	return value, err
}
