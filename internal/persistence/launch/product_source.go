package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/persistence/contentquery"
	"retrom/internal/persistence/dbexec"
	application "retrom/internal/service/launch"
)

func productCreationSource(
	ctx context.Context,
	executor dbexec.Executor,
	command application.ProductCreateCommand,
	save *application.ProductSave,
) (application.ProductSource, bool, error) {
	coreID := ""
	if command.Request.CoreID != nil {
		coreID = *command.Request.CoreID
	} else if save != nil {
		coreID = save.SourceCoreID
	}
	var source application.ProductSource
	var checkpoint *string
	err := executor.QueryRowContext(ctx, `SELECT game.id,instance.id,instance.platform_id,core.id,binding.binding_id,
 target.provider_id,target.target_id,provider.bundle_sha256,binding.delivery_profile,
 game.content_kind,game.source_manifest_digest,game.version,`+contentquery.BindingPolicySQL+`,
 COALESCE(variant.id,''),COALESCE(variant.status,''),COALESCE(variant.dependency_snapshot_json,''),
 COALESCE(variant.compatibility_code,''),variant.dat_version_id,target.checkpoint_json,
 (SELECT id FROM dat_versions WHERE provider_id=target.provider_id AND target_id=target.target_id AND is_active=1)
FROM games game
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platform_cores platform_core ON platform_core.platform_id=instance.platform_id AND platform_core.enabled=1
JOIN cores core ON core.id=platform_core.core_id AND core.enabled=1
LEFT JOIN game_variants variant ON variant.game_id=game.id AND variant.core_id=core.id
JOIN runtime_target_bindings binding ON binding.core_id=core.id AND binding.launch_policy!='DISABLED'
 AND (variant.id IS NULL OR (binding.provider_id=variant.provider_id AND binding.target_id=variant.target_id))
JOIN runtime_targets target ON target.provider_id=binding.provider_id AND target.target_id=binding.target_id
JOIN runtime_providers provider ON provider.provider_id=target.provider_id
JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
 AND binding_platform.platform_id=instance.platform_id AND binding_platform.core_id=core.id
JOIN runtime_binding_content_kinds binding_kind ON binding_kind.binding_id=binding.binding_id
 AND binding_kind.content_kind=game.content_kind
WHERE game.id=? AND game.status='PUBLISHED' AND instance.enabled=1 AND instance.deleted_at_ms IS NULL
 AND core.id=CASE WHEN ?='' THEN instance.default_core_id ELSE ? END
LIMIT 1`, command.Request.GameID, coreID, coreID).Scan(
		&source.GameID, &source.InstanceID, &source.PlatformID, &source.CoreID, &source.BindingID,
		&source.ProviderID, &source.TargetID, &source.BundleSHA256, &source.DeliveryProfile,
		&source.ContentKind, &source.SourceManifestDigest, &source.GameVersion, contentquery.ScanPolicy(
			&source.ContentPolicy,
		),
		&source.VariantID, &source.VariantStatus, &source.DependencySnapshot, &source.CompatibilityCode,
		&source.DATVersionID, &checkpoint, &source.ActiveDATVersionID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ProductSource{}, false, nil
	}
	if err != nil {
		return application.ProductSource{}, false, fmt.Errorf("query product source: %w", err)
	}
	if checkpoint != nil {
		var contract struct {
			ReadFormats []string `json:"readFormats"`
		}
		if err := json.Unmarshal([]byte(*checkpoint), &contract); err != nil {
			return application.ProductSource{}, false, fmt.Errorf("decode product checkpoint contract: %w", err)
		}
		source.ReadFormats = contract.ReadFormats
	}
	return source, true, nil
}

func productCreationOwner(
	ctx context.Context,
	executor dbexec.Executor,
	command application.ProductCreateCommand,
) (bool, error) {
	if command.ActorID == "" {
		return true, nil
	}
	var valid bool
	err := executor.QueryRowContext(ctx, `
SELECT EXISTS(SELECT 1 FROM users WHERE id=? AND profile_id=? AND status='ENABLED')`,
		command.ActorID, command.ProfileID,
	).Scan(
		&valid,
	)
	if err != nil {
		return false, fmt.Errorf("read product owner: %w", err)
	}
	return valid, nil
}
