package mediaaccess

const reviewAssetsSQL = `
SELECT blob.sha256,asset.media_type,'CANDIDATE',asset.status,COALESCE(item.state,''),COALESCE(game.status,''),
EXISTS(SELECT 1 FROM review_events event WHERE event.import_item_id=item.id
 AND event.event_type IN ('APPROVED','DISCARDED'))
FROM scrape_candidate_assets asset JOIN blobs blob ON blob.id=asset.blob_id
JOIN scrape_candidates candidate ON candidate.id=asset.scrape_candidate_id
JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id
LEFT JOIN import_items item ON item.id=run.import_item_id LEFT JOIN games game ON game.id=run.game_id
WHERE asset.id=?
UNION ALL
SELECT blob.sha256,asset.media_type,'UPLOAD','',item.state,'',
EXISTS(SELECT 1 FROM review_events event WHERE event.import_item_id=item.id
 AND event.event_type IN ('APPROVED','DISCARDED'))
FROM review_uploaded_assets asset JOIN blobs blob ON blob.id=asset.blob_id
JOIN import_items item ON item.id=asset.import_item_id WHERE asset.id=?
UNION ALL
SELECT blob.sha256,asset.media_type,'SCREENSHOT','',item.state,'',
EXISTS(SELECT 1 FROM review_events event WHERE event.import_item_id=item.id
 AND event.event_type IN ('APPROVED','DISCARDED'))
FROM review_runtime_screenshots asset JOIN blobs blob ON blob.id=asset.blob_id
JOIN import_items item ON item.id=asset.import_item_id WHERE asset.id=?`

const sourceAssetsSQL = `
SELECT blob.sha256,asset.media_type,'PEGASUS',asset.state,item.state,'',
EXISTS(SELECT 1 FROM review_events event WHERE event.import_item_id=item.id
 AND event.event_type IN ('APPROVED','DISCARDED'))
FROM pegasus_import_item_assets asset JOIN blobs blob ON blob.id=asset.blob_id
JOIN pegasus_import_items source ON source.id=asset.item_id
JOIN import_items item ON item.id=source.library_import_item_id
WHERE source.id=? AND asset.kind=?
UNION ALL
SELECT blob.sha256,asset.media_type,'EMULATIONSTATION',asset.state,item.state,'',
EXISTS(SELECT 1 FROM review_events event WHERE event.import_item_id=item.id
 AND event.event_type IN ('APPROVED','DISCARDED'))
FROM emulationstation_import_item_assets asset JOIN blobs blob ON blob.id=asset.blob_id
JOIN emulationstation_import_items source ON source.id=asset.item_id
JOIN import_items item ON item.id=source.library_import_item_id
WHERE source.id=? AND asset.kind=?`
