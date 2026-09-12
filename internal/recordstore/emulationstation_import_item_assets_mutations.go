package recordstore

import (
	"context"
	"database/sql"
)

func UpdateEmulationstationImportItemAssets(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"emulationstation_import_item_assets",
		"item_id,kind,blob_id,created_at_ms,height_px,media_type,payload_released_at_ms,"+
			"relative_path,resolution_method,size_bytes,source_facts_digest,state,warning_code,"+
			"width_px",
		EmulationstationImportItemAssetsUpdateRule,
	)
}

const EmulationstationImportItemAssetsUpdateRule = `
WITH previous(item_id,kind,blob_id,created_at_ms,height_px,media_type,payload_released_at_ms,
relative_path,resolution_method,size_bytes,source_facts_digest,state,warning_code,width_px) AS (VALUES(?,
?,?,?,?,?,?,?,?,?,?,?,?,?))
SELECT CASE
-- emulationstation_asset_snapshot_update
WHEN ((candidate.item_id IS NOT previous.item_id OR candidate.kind IS NOT previous.kind OR
candidate.resolution_method IS NOT previous.resolution_method OR candidate.relative_path IS NOT
previous.relative_path OR candidate.size_bytes IS NOT previous.size_bytes OR
candidate.source_facts_digest IS NOT previous.source_facts_digest OR candidate.media_type IS NOT
previous.media_type OR candidate.width_px IS NOT previous.width_px OR candidate.height_px IS NOT
previous.height_px OR candidate.created_at_ms IS NOT previous.created_at_ms) AND (1=1)) THEN
'immutable EmulationStation asset snapshot'
-- emulationstation_asset_state_update
WHEN ((candidate.state IS NOT previous.state OR candidate.blob_id IS NOT previous.blob_id OR
candidate.payload_released_at_ms IS NOT previous.payload_released_at_ms OR candidate.warning_code IS NOT
previous.warning_code) AND (previous.state<>candidate.state AND NOT (
  previous.state='DISCOVERED' AND candidate.state IN ('COPIED','SOURCE_CHANGED','READ_FAILED') OR
  previous.state='COPIED' AND candidate.state='PAYLOAD_RELEASED'
) OR candidate.state='COPIED' AND candidate.blob_id IS NULL
  OR candidate.state IN ('DISCOVERED','MISSING','INVALID','TOO_LARGE','SOURCE_CHANGED','READ_FAILED',
'PAYLOAD_RELEASED') AND candidate.blob_id IS NOT NULL)) THEN 'invalid EmulationStation asset transition'
ELSE '' END
FROM emulationstation_import_item_assets candidate CROSS JOIN previous
WHERE candidate.item_id=previous.item_id AND candidate.kind=previous.kind`

func DeleteEmulationstationImportItemAssets(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"emulationstation_import_item_assets",
		"item_id,kind",
		EmulationstationImportItemAssetsDeleteRule,
	)
}

const EmulationstationImportItemAssetsDeleteRule = `
WITH previous(item_id,kind) AS (VALUES(?,?))
SELECT CASE
-- emulationstation_asset_delete
WHEN (NOT EXISTS(
  SELECT 1 FROM emulationstation_import_items item
  JOIN emulationstation_imports import ON import.id=item.import_id
  WHERE item.id=previous.item_id AND (
    import.state='SCANNING' OR import.state='AWAITING_MAPPING' AND item.execution_state='PENDING'
    OR import.state='EXPIRED' AND item.execution_state='CANCELLED'
  )
)) THEN 'EmulationStation asset snapshot is frozen'
ELSE '' END
FROM previous`
