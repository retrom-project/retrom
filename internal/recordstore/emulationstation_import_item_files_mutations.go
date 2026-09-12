package recordstore

import (
	"context"
	"database/sql"
)

func UpdateEmulationstationImportItemFiles(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"emulationstation_import_item_files",
		"item_id,ordinal,blob_id,created_at_ms,declared_kind,logical_name,payload_released_at_ms,"+
			"relative_path,role,size_bytes,source_archive_blob_id,source_archive_entry_ordinal,"+
			"source_facts_digest,state",
		EmulationstationImportItemFilesUpdateRule,
	)
}

const EmulationstationImportItemFilesUpdateRule = `
WITH previous(item_id,ordinal,blob_id,created_at_ms,declared_kind,logical_name,payload_released_at_ms,
relative_path,role,size_bytes,source_archive_blob_id,source_archive_entry_ordinal,source_facts_digest,
state) AS (VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?))
SELECT CASE
-- emulationstation_file_snapshot_update
WHEN ((candidate.item_id IS NOT previous.item_id OR candidate.ordinal IS NOT previous.ordinal OR
candidate.declared_kind IS NOT previous.declared_kind OR candidate.relative_path IS NOT
previous.relative_path OR candidate.size_bytes IS NOT previous.size_bytes OR
candidate.source_facts_digest IS NOT previous.source_facts_digest OR candidate.created_at_ms IS NOT
previous.created_at_ms) AND (1=1)) THEN 'immutable EmulationStation file snapshot'
-- emulationstation_file_state_update
WHEN ((candidate.state IS NOT previous.state OR candidate.blob_id IS NOT previous.blob_id OR
candidate.source_archive_blob_id IS NOT previous.source_archive_blob_id OR
candidate.source_archive_entry_ordinal IS NOT previous.source_archive_entry_ordinal OR candidate.role IS
NOT previous.role OR candidate.logical_name IS NOT previous.logical_name OR
candidate.payload_released_at_ms IS NOT previous.payload_released_at_ms) AND
(previous.state<>candidate.state AND NOT (
  previous.state='DISCOVERED' AND candidate.state IN ('COPIED','SOURCE_CHANGED','READ_FAILED',
'UNSUPPORTED') OR
  previous.state='COPIED' AND candidate.state='PAYLOAD_RELEASED'
) OR candidate.state='COPIED' AND candidate.blob_id IS NULL
  OR candidate.state IN ('DISCOVERED','SOURCE_CHANGED','READ_FAILED','UNSUPPORTED','PAYLOAD_RELEASED')
AND candidate.blob_id IS NOT NULL)) THEN 'invalid EmulationStation file transition'
ELSE '' END
FROM emulationstation_import_item_files candidate CROSS JOIN previous
WHERE candidate.item_id=previous.item_id AND candidate.ordinal=previous.ordinal`

func DeleteEmulationstationImportItemFiles(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"emulationstation_import_item_files",
		"item_id,ordinal",
		EmulationstationImportItemFilesDeleteRule,
	)
}

const EmulationstationImportItemFilesDeleteRule = `
WITH previous(item_id,ordinal) AS (VALUES(?,?))
SELECT CASE
-- emulationstation_file_delete
WHEN (NOT EXISTS(
  SELECT 1 FROM emulationstation_import_items item
  JOIN emulationstation_imports import ON import.id=item.import_id
  WHERE item.id=previous.item_id AND (
    import.state='SCANNING' OR import.state='AWAITING_MAPPING' AND item.execution_state='PENDING'
    OR import.state='EXPIRED' AND item.execution_state='CANCELLED'
  )
)) THEN 'EmulationStation file snapshot is frozen'
ELSE '' END
FROM previous`
