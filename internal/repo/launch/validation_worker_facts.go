package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"retrom/internal/capability/format/arcadedat"
	application "retrom/internal/model/launch"
	"retrom/internal/repo/contentquery"
)

func (records validationWorkerRecords) Facts(
	ctx context.Context,
	inputs application.ValidationInputs,
) (application.ValidationFacts, error) {
	var facts application.ValidationFacts
	source := &facts.Content.Source
	err := records.executor.QueryRowContext(ctx, `
 SELECT game.id,game.version,game.source_manifest_digest,game.content_kind,
 instance.platform_id,variant.id,variant.core_id,variant.provider_id,variant.target_id,variant.dat_version_id,
 variant.dependency_snapshot_json,variant.version,
 COALESCE((SELECT logical_name FROM game_files WHERE game_id=game.id AND role IN ('CONTENT','DISC')
 ORDER BY CASE role WHEN 'CONTENT' THEN 0 ELSE 1 END,sort_order,logical_name LIMIT 1),''),
 `+contentquery.BindingPolicySQL+`,
 EXISTS(SELECT 1 FROM runtime_targets target JOIN runtime_providers provider ON provider.provider_id=target.provider_id
 WHERE target.provider_id=variant.provider_id AND target.target_id=variant.target_id)
 AND binding.launch_policy!='DISABLED' AND EXISTS(SELECT 1 FROM runtime_binding_platforms bp
 WHERE bp.binding_id=binding.binding_id AND bp.platform_id=instance.platform_id AND bp.core_id=variant.core_id)
 AND EXISTS(SELECT 1 FROM runtime_binding_content_kinds bk
 WHERE bk.binding_id=binding.binding_id AND bk.content_kind=game.content_kind),
 EXISTS(SELECT 1 FROM platform_cores pc JOIN cores core ON core.id=pc.core_id
 WHERE pc.platform_id=instance.platform_id AND pc.core_id=variant.core_id AND pc.enabled=1 AND core.enabled=1)
 AND instance.enabled=1 AND instance.deleted_at_ms IS NULL
 FROM games game JOIN platform_instances instance ON instance.id=game.platform_instance_id
 JOIN game_variants variant ON variant.game_id=game.id AND variant.id=?
 LEFT JOIN runtime_target_bindings binding ON binding.core_id=variant.core_id
 AND binding.provider_id=variant.provider_id AND binding.target_id=variant.target_id
 WHERE game.id=? AND game.status='PUBLISHED'`, inputs.GameVariantID, inputs.GameID).Scan(
		&source.GameID, &source.GameVersion, &source.SourceManifestDigest, &source.ContentKind, &source.PlatformID,
		&source.VariantID,
		&source.CoreID,
		&source.ProviderID,
		&source.TargetID,
		&source.DATVersionID,
		&source.DependencySnapshot, &facts.VariantVersion,
		&source.ValidationLogicalName, contentquery.ScanPolicy(&source.ContentPolicy),
		&facts.BindingFound,
		&facts.RelationshipEnabled,
	)
	if err != nil {
		return application.ValidationFacts{}, fmt.Errorf("read validation source: %w", err)
	}
	source.ActiveDATVersionID = inputs.DATVersionID
	content, err := ProductContentSnapshot(ctx, records.executor, *source)
	if err != nil {
		return application.ValidationFacts{}, err
	}
	facts.Found = true
	facts.Content = content
	if source.ProviderID != "retrom-runtime" || source.TargetID != "scummvm" {
		bios, err := ProductBIOSFacts(ctx, records.executor, *source, inputs.DATVersionID)
		if err != nil {
			return application.ValidationFacts{}, err
		}
		facts.Content.ValidationBIOS = bios
	}
	if arcadedat.SupportsCore(source.CoreID) && inputs.DATVersionID != nil {
		machine := strings.TrimSuffix(filepath.Base(source.ValidationLogicalName), filepath.Ext(source.ValidationLogicalName))
		err := records.executor.QueryRowContext(ctx, `SELECT classification FROM dat_machines
 WHERE dat_version_id=? AND lower(machine_name)=lower(?)`, *inputs.DATVersionID, machine).Scan(&facts.Classification)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return application.ValidationFacts{}, fmt.Errorf("read validation DAT machine: %w", err)
		}
	}
	return facts, nil
}
