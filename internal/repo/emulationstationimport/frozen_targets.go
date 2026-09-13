package emulationstationimport

import (
	"context"
	"fmt"
)

func (records frozenSourceRecords) targetsValid(ctx context.Context, id string) (bool, error) {
	var valid bool
	err := records.executor.QueryRowContext(ctx, `
SELECT NOT EXISTS(
 SELECT 1 FROM emulationstation_import_collections collection
 WHERE collection.import_id=? AND collection.mapping_action='IMPORT' AND NOT EXISTS(
  SELECT 1 FROM platform_instances instance
  JOIN platforms platform ON platform.id=instance.platform_id AND platform.enabled=1
  JOIN cores core ON core.id=instance.default_core_id AND core.enabled=1
  JOIN runtime_target_bindings binding ON binding.core_id=instance.default_core_id AND binding.launch_policy!='DISABLED'
   AND binding.provider_id=collection.target_provider_id AND binding.target_id=collection.target_id
  JOIN runtime_binding_platforms membership ON membership.binding_id=binding.binding_id
   AND membership.platform_id=instance.platform_id
  JOIN runtime_targets target ON target.provider_id=binding.provider_id AND target.target_id=binding.target_id
  WHERE instance.id=collection.target_platform_instance_id AND instance.enabled=1 AND instance.deleted_at_ms IS NULL
   AND instance.version=collection.target_platform_instance_version
AND instance.platform_id=collection.target_platform_id
   AND instance.default_core_id=collection.target_default_core_id
   AND (collection.target_dat_version_id IS NULL OR EXISTS(
    SELECT 1 FROM dat_versions dat WHERE dat.id=collection.target_dat_version_id AND dat.is_active=1
     AND dat.provider_id=collection.target_provider_id AND dat.target_id=collection.target_id
   ))
 )
)`, id).Scan(&valid)
	if err != nil {
		return false, fmt.Errorf("read EmulationStation frozen target eligibility: %w", err)
	}
	return valid, nil
}
