package mediaaccess

const reviewAssetsSQL = `
SELECT blob.id,blob.sha256,asset.media_type,'CANDIDATE',asset.status,COALESCE(item.state,''),COALESCE(game.status,''),
item.state IN ('PUBLISHED','DISCARDED','CANCELLED','FAILED_FINAL')
FROM scrape_candidate_assets asset JOIN stored_files blob ON blob.id=asset.blob_id
JOIN scrape_candidates candidate ON candidate.id=asset.scrape_candidate_id
JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id
LEFT JOIN import_items item ON item.id=run.import_item_id LEFT JOIN games game ON game.id=run.game_id
WHERE asset.id=?
UNION ALL
SELECT blob.id,blob.sha256,asset.media_type,'UPLOAD','',item.state,'',
item.state IN ('PUBLISHED','DISCARDED','CANCELLED','FAILED_FINAL')
FROM review_uploaded_assets asset JOIN stored_files blob ON blob.id=asset.blob_id
JOIN import_items item ON item.id=asset.import_item_id WHERE asset.id=?
UNION ALL
SELECT blob.id,blob.sha256,asset.media_type,'SCREENSHOT','',item.state,'',
item.state IN ('PUBLISHED','DISCARDED','CANCELLED','FAILED_FINAL')
FROM review_runtime_screenshots asset JOIN stored_files blob ON blob.id=asset.blob_id
JOIN import_items item ON item.id=asset.import_item_id WHERE asset.id=?`

const sourceAssetsSQL = `
SELECT blob.id,blob.sha256,asset.media_type,'SOURCE','COPIED',item.state,'',
item.state IN ('PUBLISHED','DISCARDED','CANCELLED','FAILED_FINAL')
FROM import_item_assets asset JOIN stored_files blob ON blob.id=asset.blob_id
JOIN import_items item ON item.id=asset.import_item_id
JOIN source_import_items source ON source.library_import_item_id=item.id
WHERE source.id=? AND asset.kind=?`
