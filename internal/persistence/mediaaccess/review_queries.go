package mediaaccess

const reviewAssetsSQL = `
SELECT blob.value,((blob.value)::jsonb #>> '{sha256}'),asset.media_type,'CANDIDATE',asset.status,
COALESCE(item.state,''),COALESCE(game.status,'')
FROM scrape_candidate_assets asset JOIN LATERAL (SELECT asset.file_record AS value) blob ON blob.value IS
NOT NULL
JOIN scrape_candidates candidate ON candidate.id=asset.scrape_candidate_id
JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id
LEFT JOIN import_items item ON item.id=run.import_item_id LEFT JOIN games game ON game.id=run.game_id
WHERE asset.id=?
UNION ALL
SELECT blob.value,((blob.value)::jsonb #>> '{sha256}'),asset.media_type,'UPLOAD','',item.state,''
FROM review_uploaded_assets asset JOIN LATERAL (SELECT asset.file_record AS value) blob ON blob.value IS NOT NULL
JOIN import_items item ON item.id=asset.import_item_id WHERE asset.id=?
UNION ALL
SELECT blob.value,((blob.value)::jsonb #>> '{sha256}'),asset.media_type,'SCREENSHOT','',item.state,''
FROM review_runtime_screenshots asset JOIN LATERAL (SELECT asset.file_record AS value) blob ON blob.value
IS NOT NULL
JOIN import_items item ON item.id=asset.import_item_id WHERE asset.id=?`

const sourceAssetsSQL = `
SELECT blob.value,((blob.value)::jsonb #>> '{sha256}'),asset.media_type,'SOURCE','COPIED',item.state,''
FROM import_item_assets asset JOIN LATERAL (SELECT asset.file_record AS value) blob ON blob.value IS NOT NULL
JOIN import_items item ON item.id=asset.import_item_id
JOIN source_import_items source ON source.library_import_item_id=item.id
WHERE source.id=? AND asset.kind=?`
