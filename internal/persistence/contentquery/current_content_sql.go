package contentquery

// CurrentContentSQL projects an item's content observations and its current
// runtime binding. It contains no historical validation records.
const CurrentContentSQL = `
SELECT item.id AS import_item_id,item.target_platform_instance_id,
 instance.version AS platform_instance_version,instance.default_core_id AS core_id,
 target.provider_id,target.target_id,
 (SELECT active.id FROM dat_versions active WHERE active.provider_id=target.provider_id
  AND active.target_id=target.target_id AND active.is_active=1) AS dat_version_id,
 item.default_dos_entry,source.source_manifest_digest,source.id AS source_snapshot_id,
 COALESCE(json_extract(item.content_analysis_json,'$.status'),'BLOCKED') AS status,
 COALESCE(json_extract(item.content_analysis_json,'$.code'),'CONTENT_ANALYSIS_UNAVAILABLE') AS compatibility_code,
 COALESCE(json_extract(item.content_analysis_json,'$.details'),'{}') AS dependency_snapshot_json
FROM import_items item
JOIN import_item_source_snapshots source ON source.id=item.effective_source_snapshot_id
JOIN platform_instances instance ON instance.id=item.target_platform_instance_id
JOIN runtime_target_bindings runtime_binding ON runtime_binding.core_id=instance.default_core_id
 AND runtime_binding.launch_policy<>'DISABLED'
JOIN runtime_binding_platforms platform_binding ON platform_binding.binding_id=runtime_binding.binding_id
 AND platform_binding.platform_id=instance.platform_id
JOIN runtime_binding_content_kinds content_binding ON content_binding.binding_id=runtime_binding.binding_id
 AND content_binding.content_kind=source.content_kind
JOIN runtime_targets target ON target.provider_id=runtime_binding.provider_id
 AND target.target_id=runtime_binding.target_id
 AND (json_extract(item.review_profile_json,'$.data.providerId') IS NULL
  OR target.provider_id=json_extract(item.review_profile_json,'$.data.providerId'))
 AND (json_extract(item.review_profile_json,'$.data.targetId') IS NULL
  OR target.target_id=json_extract(item.review_profile_json,'$.data.targetId'))
`
