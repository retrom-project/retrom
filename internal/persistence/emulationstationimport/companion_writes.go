package emulationstationimport

import (
	"context"

	application "retrom/internal/service/emulationstationimport"
)

func (records companionRecords) Register(ctx context.Context, change application.CompanionBinding) (string, error) {
	if err := executionRecords(records).Fence(ctx, change.Before.Before.Execution, change.NowMS); err != nil {
		return "", err
	}
	result, err := records.executor.ExecContext(
		ctx,
		`UPDATE emulationstation_import_items SET version=version WHERE `+ownedItemPredicate+` AND execution_state='COPYING'`,
		ownedItemArguments(change.Before.Before)...,
	)
	if err := requireMappingChange(result, err, application.ErrVersionConflict); err != nil {
		return "", err
	}
	if err := records.fenceMapping(ctx, change.Before); err != nil {
		return "", err
	}
	if err := records.fenceCandidate(ctx, change); err != nil {
		return "", err
	}
	return registerVerifiedMaterial(ctx, records.executor, change.Blob, "application/zip", change.NowMS)
}

func (records companionRecords) fenceCandidate(ctx context.Context, change application.CompanionBinding) error {
	file, item := change.File, change.Before.Before.Item
	result, err := records.executor.ExecContext(
		ctx,
		`UPDATE emulationstation_import_item_files SET ordinal=ordinal
WHERE item_id=? AND ordinal=? AND relative_path=? AND size_bytes=? AND source_facts_digest=?
AND EXISTS(SELECT 1 FROM emulationstation_import_items candidate
 JOIN emulationstation_import_collections collection ON collection.id=candidate.collection_id
 WHERE candidate.id=emulationstation_import_item_files.item_id AND candidate.id<>? AND candidate.import_id=?
 AND candidate.collection_id=? AND candidate.discovery_state='READY'
 AND collection.mapping_action='IMPORT' AND collection.target_platform_instance_id=?
 AND collection.target_dat_version_id=?
 AND (SELECT count(*) FROM emulationstation_import_item_files own WHERE own.item_id=candidate.id)=1)`,

		file.ItemID,
		file.Ordinal,
		file.Path,
		file.Size,
		file.Facts,
		item.ID,
		item.ImportID,
		file.CollectionID,
		item.TargetPlatformID,
		item.TargetDATVersionID,
	)
	return requireMappingChange(result, err, application.ErrVersionConflict)
}

func (records companionRecords) fenceMapping(ctx context.Context, owner application.CompanionOwner) error {
	target := owner.Mapping
	result, err := records.executor.ExecContext(
		ctx,
		`UPDATE emulationstation_import_collections SET updated_at_ms=updated_at_ms
WHERE id=? AND import_id=? AND mapping_action='IMPORT' AND target_platform_instance_id=?
AND target_platform_instance_version=? AND target_platform_id=? AND target_default_core_id=?
AND target_provider_id=? AND target_id=? AND target_dat_version_id=?
AND EXISTS(SELECT 1 FROM emulationstation_imports plan WHERE plan.id=import_id AND plan.mapping_version=?)
AND EXISTS(SELECT 1 FROM platform_instances instance
 JOIN platforms platform ON platform.id=instance.platform_id AND platform.enabled=1
 JOIN cores core ON core.id=instance.default_core_id AND core.enabled=1
 JOIN runtime_target_bindings binding ON binding.core_id=core.id AND binding.launch_policy!='DISABLED'
 JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
 AND binding_platform.platform_id=instance.platform_id
 WHERE instance.id=target_platform_instance_id AND instance.version=target_platform_instance_version
 AND instance.platform_id=target_platform_id AND instance.default_core_id=target_default_core_id
 AND instance.enabled=1 AND instance.deleted_at_ms IS NULL
 AND binding.provider_id=target_provider_id AND binding.target_id=emulationstation_import_collections.target_id)
AND EXISTS(SELECT 1 FROM dat_versions dat WHERE dat.id=target_dat_version_id
 AND dat.provider_id=target_provider_id AND dat.target_id=emulationstation_import_collections.target_id
 AND dat.is_active=1)`,

		owner.CollectionID,
		owner.Before.Item.ImportID,
		target.InstanceID,
		target.InstanceVersion,
		target.PlatformID,
		target.CoreID,

		target.ProviderID,
		target.TargetID,
		target.DATVersionID,
		owner.MappingVersion,
	)
	return requireMappingChange(result, err, application.ErrVersionConflict)
}
