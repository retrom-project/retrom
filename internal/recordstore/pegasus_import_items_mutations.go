package recordstore

import (
	"context"
	"database/sql"
)

func UpdatePegasusImportItems(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"pegasus_import_items",
		"id,collection_id,content_kind,created_at_ms,discovery_code,discovery_state,"+
			"execution_state,game_ordinal,import_id,library_import_item_id,library_import_job_id,"+
			"metadata_json,metadata_relative_path,published_game_id,source_key,"+
			"source_manifest_digest,source_manifest_json,title",
		PegasusImportItemsUpdateRule,
	)
}

const PegasusImportItemsUpdateRule = `
WITH previous(id,collection_id,content_kind,created_at_ms,discovery_code,discovery_state,execution_state,
game_ordinal,import_id,library_import_item_id,library_import_job_id,metadata_json,metadata_relative_path,
published_game_id,source_key,source_manifest_digest,source_manifest_json,title) AS (VALUES(?,?,?,?,?,?,?,
?,?,?,?,?,?,?,?,?,?,?))
SELECT CASE
-- pegasus_item_library_review_cross_owner_update
WHEN ((candidate.library_import_item_id IS NOT previous.library_import_item_id) AND
(candidate.library_import_item_id IS NOT NULL AND EXISTS(
  SELECT 1 FROM emulationstation_import_items WHERE
library_import_item_id=candidate.library_import_item_id
))) THEN 'server source review already owned'
-- pegasus_item_manifest_update
WHEN ((candidate.content_kind IS NOT previous.content_kind OR candidate.source_manifest_json IS NOT
previous.source_manifest_json OR candidate.source_manifest_digest IS NOT previous.source_manifest_digest
OR candidate.library_import_job_id IS NOT previous.library_import_job_id OR
candidate.library_import_item_id IS NOT previous.library_import_item_id) AND (previous.execution_state
NOT IN ('COPYING','VALIDATING') OR candidate.execution_state NOT IN ('VALIDATING','REVIEW_PENDING')))
THEN 'invalid Pegasus manifest transition'
-- pegasus_item_published_update
WHEN ((candidate.execution_state IS NOT previous.execution_state OR candidate.published_game_id IS NOT
previous.published_game_id) AND (candidate.execution_state='PUBLISHED' AND (
  candidate.published_game_id IS NULL OR NOT EXISTS(
    SELECT 1 FROM games game
    WHERE game.id=candidate.published_game_id AND game.metadata_source_kind='SERVER_PEGASUS_IMPORT'
    AND game.metadata_source_ref_id=candidate.id AND game.content_source_kind='SERVER_PEGASUS_IMPORT'
    AND game.content_source_ref_id=candidate.id
  )
))) THEN 'invalid Pegasus published game'
-- pegasus_item_review_pending_update
WHEN ((candidate.execution_state IS NOT previous.execution_state) AND
(candidate.execution_state='REVIEW_PENDING' AND (
  candidate.library_import_job_id IS NULL OR candidate.library_import_item_id IS NULL OR NOT EXISTS(
    SELECT 1 FROM import_items item
    WHERE item.id=candidate.library_import_item_id AND item.import_job_id=candidate.library_import_job_id
    AND item.state='REVIEW_PENDING'
  )
))) THEN 'invalid Pegasus review handoff'
-- pegasus_item_snapshot_update
WHEN (candidate.import_id<>previous.import_id OR candidate.collection_id IS NOT previous.collection_id OR
  candidate.metadata_relative_path<>previous.metadata_relative_path OR
candidate.game_ordinal<>previous.game_ordinal OR
  candidate.source_key<>previous.source_key OR candidate.title<>previous.title OR
candidate.discovery_state<>previous.discovery_state OR
  candidate.metadata_json<>previous.metadata_json OR candidate.discovery_code IS NOT
previous.discovery_code OR
  candidate.created_at_ms<>previous.created_at_ms) THEN 'immutable Pegasus item snapshot'
-- pegasus_item_review_discarded_update
WHEN ((candidate.execution_state IS NOT previous.execution_state) AND
(candidate.execution_state='REVIEW_DISCARDED'
AND NOT EXISTS(SELECT 1 FROM import_batch_discards batch
 WHERE batch.kind='PEGASUS' AND batch.import_id=candidate.import_id
 AND (candidate.library_import_item_id IS NULL OR EXISTS(SELECT 1 FROM import_items item
 WHERE item.id=candidate.library_import_item_id AND item.state IN ('DISCARDED','FAILED_FINAL',
'CANCELLED'))))
AND NOT EXISTS(
  SELECT 1 FROM import_items item WHERE item.id=candidate.library_import_item_id AND
item.state='DISCARDED'
))) THEN 'invalid Pegasus review discard'
ELSE '' END
FROM pegasus_import_items candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
