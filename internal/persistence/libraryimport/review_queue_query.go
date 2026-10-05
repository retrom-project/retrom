package libraryimport

import "retrom/internal/persistence/contentquery"

const reviewQueueSelect = `
SELECT i.id,
d.review_version,
i.import_job_id,
((d.metadata_json)::jsonb #>> '{title}'),
COALESCE(((i.source_manifest_json)::jsonb #>> '{0,logicalName}'),
((i.source_manifest_json)::jsonb #>> '{files,0,logicalName}'),
((d.metadata_json)::jsonb #>> '{title}')),
pi.id,
pi.name,
v.status,
v.compatibility_code,
d.review_updated_at_ms,
(SELECT count(*)
FROM scrape_candidates c
JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
WHERE r.import_item_id=i.id
AND r.state='COMPLETED'),
(SELECT COALESCE(sum((((b.value)::jsonb #>> '{size_bytes}'))::bigint),0)
 FROM import_item_source_snapshot_files source_file
 JOIN LATERAL (SELECT source_file.file_record AS value) b ON b.value IS NOT NULL
 WHERE source_file.source_snapshot_id=d.effective_source_snapshot_id),
(SELECT ((b.value)::jsonb #>> '{md5}')
 FROM import_item_source_snapshot_files source_file
 JOIN LATERAL (SELECT source_file.file_record AS value) b ON b.value IS NOT NULL
 WHERE source_file.source_snapshot_id=d.effective_source_snapshot_id
 ORDER BY CASE source_file.role WHEN 'CONTENT' THEN 0 WHEN 'DOS_SOURCE' THEN 1 ELSE 2 END,
 source_file.sort_order,
 source_file.logical_name
 LIMIT 1),
COALESCE(d.cover_uploaded_asset_id,(SELECT asset.id
 FROM scrape_candidate_assets asset
 JOIN scrape_candidates candidate ON candidate.id=asset.scrape_candidate_id
 JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id
 WHERE run.import_item_id=i.id
 AND run.state='COMPLETED'
 AND asset.status='READY'
 AND asset.kind_hint='COVER'
 ORDER BY CASE WHEN asset.id=d.cover_candidate_asset_id THEN 0 ELSE 1 END,
 run.completed_at_ms DESC,
 asset.ordinal,
 asset.id
 LIMIT 1))
,source.id,source.import_id,source_collection.name,
EXISTS(
 SELECT 1 FROM import_item_assets source_asset
 WHERE source_asset.import_item_id=i.id AND source_asset.kind='COVER'
 AND source_asset.file_record IS NOT NULL
)
FROM import_items i
JOIN import_items d ON d.id=i.id
JOIN platform_instances pi ON pi.id=d.target_platform_instance_id
	LEFT JOIN source_import_items source ON source.library_import_item_id=i.id
	LEFT JOIN source_import_collections source_collection ON source_collection.id=source.collection_id
LEFT JOIN (` + contentquery.CurrentContentSQL + `) v ON v.import_item_id=i.id
WHERE i.state='REVIEW_PENDING'
AND (source.id IS NULL OR source.execution_state='REVIEW_PENDING')
`
