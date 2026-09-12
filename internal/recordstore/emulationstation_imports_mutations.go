package recordstore

import (
	"context"
	"database/sql"
)

func UpdateEmulationstationImports(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"emulationstation_imports",
		"id,blocked_item_count,cancel_reason,cancelled_item_count,completed_at_ms,created_at_ms,"+
			"created_by_user_id,existing_item_count,expires_at_ms,failed_item_count,import_job_id,"+
			"last_error_code,mapping_version,phase,published_item_count,release_year_max,retryable,"+
			"review_discarded_item_count,review_pending_item_count,root_config_digest,root_id,"+
			"root_label_snapshot,scan_completed_at_ms,scan_job_id,skipped_mapping_item_count,"+
			"source_relative_path,source_snapshot_digest,started_at_ms,state,updated_at_ms,version",
		EmulationstationImportsUpdateRule,
	)
}

const EmulationstationImportsUpdateRule = `
WITH previous(id,blocked_item_count,cancel_reason,cancelled_item_count,completed_at_ms,created_at_ms,
created_by_user_id,existing_item_count,expires_at_ms,failed_item_count,import_job_id,last_error_code,
mapping_version,phase,published_item_count,release_year_max,retryable,review_discarded_item_count,
review_pending_item_count,root_config_digest,root_id,root_label_snapshot,scan_completed_at_ms,
scan_job_id,skipped_mapping_item_count,source_relative_path,source_snapshot_digest,started_at_ms,state,
updated_at_ms,version) AS (VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?))
SELECT CASE
-- emulationstation_import_identity_update
WHEN ((candidate.id IS NOT previous.id OR candidate.root_id IS NOT previous.root_id OR
candidate.root_label_snapshot IS NOT previous.root_label_snapshot OR candidate.source_relative_path IS
NOT previous.source_relative_path OR candidate.root_config_digest IS NOT previous.root_config_digest OR
candidate.release_year_max IS NOT previous.release_year_max OR candidate.scan_job_id IS NOT
previous.scan_job_id OR candidate.created_by_user_id IS NOT previous.created_by_user_id OR
candidate.created_at_ms IS NOT previous.created_at_ms OR candidate.expires_at_ms IS NOT
previous.expires_at_ms) AND (candidate.id<>previous.id OR candidate.root_id<>previous.root_id OR
candidate.root_label_snapshot<>previous.root_label_snapshot
  OR candidate.source_relative_path<>previous.source_relative_path OR
candidate.root_config_digest<>previous.root_config_digest
  OR candidate.release_year_max<>previous.release_year_max OR candidate.scan_job_id<>previous.scan_job_id
  OR candidate.created_by_user_id<>previous.created_by_user_id OR
candidate.created_at_ms<>previous.created_at_ms
  OR candidate.expires_at_ms<>previous.expires_at_ms)) THEN 'immutable EmulationStation import identity'
-- emulationstation_import_job_update
WHEN ((candidate.import_job_id IS NOT previous.import_job_id) AND (previous.import_job_id IS NOT
candidate.import_job_id AND (
  previous.import_job_id IS NOT NULL OR candidate.import_job_id IS NULL OR NOT EXISTS(
    SELECT 1 FROM jobs job WHERE job.id=candidate.import_job_id
    AND job.kind='SERVER_EMULATIONSTATION_IMPORT'
    AND job.scope_type='EMULATIONSTATION_IMPORT' AND job.scope_id=candidate.id
  )
))) THEN 'invalid EmulationStation import job'
-- emulationstation_import_lifecycle_update
WHEN ((candidate.state IS NOT previous.state OR candidate.phase IS NOT previous.phase OR
candidate.source_snapshot_digest IS NOT previous.source_snapshot_digest OR candidate.import_job_id IS
NOT previous.import_job_id OR candidate.scan_completed_at_ms IS NOT previous.scan_completed_at_ms OR
candidate.started_at_ms IS NOT previous.started_at_ms OR candidate.completed_at_ms IS NOT
previous.completed_at_ms OR candidate.cancel_reason IS NOT previous.cancel_reason OR
candidate.last_error_code IS NOT previous.last_error_code OR candidate.retryable IS NOT
previous.retryable) AND (NOT (
  candidate.state='SCANNING' AND candidate.import_job_id IS NULL AND candidate.source_snapshot_digest IS
NULL
    AND candidate.scan_completed_at_ms IS NULL AND candidate.started_at_ms IS NULL AND
candidate.completed_at_ms IS NULL
    AND candidate.phase IN ('DISCOVERING_GAMELISTS','PARSING_GAMELISTS','RESOLVING_SOURCES')
  OR candidate.state='AWAITING_MAPPING' AND candidate.import_job_id IS NULL AND
candidate.source_snapshot_digest IS NOT NULL
    AND candidate.scan_completed_at_ms IS NOT NULL AND candidate.started_at_ms IS NULL AND
candidate.completed_at_ms IS NULL AND candidate.phase IS NULL
  OR candidate.state='QUEUED' AND candidate.import_job_id IS NOT NULL AND
candidate.source_snapshot_digest IS NOT NULL
    AND candidate.scan_completed_at_ms IS NOT NULL AND candidate.completed_at_ms IS NULL AND
candidate.phase IS NULL
  OR candidate.state='RUNNING' AND candidate.import_job_id IS NOT NULL AND
candidate.source_snapshot_digest IS NOT NULL
    AND candidate.scan_completed_at_ms IS NOT NULL AND candidate.started_at_ms IS NOT NULL AND
candidate.completed_at_ms IS NULL
    AND candidate.phase IN ('COPYING_CONTENT','VALIDATING','PREPARING_REVIEWS')
  OR candidate.state='CANCEL_REQUESTED' AND candidate.import_job_id IS NOT NULL AND
candidate.cancel_reason IS NOT NULL
    AND candidate.completed_at_ms IS NULL AND candidate.phase IN ('COPYING_CONTENT','VALIDATING',
'PREPARING_REVIEWS')
  OR candidate.state IN ('COMPLETED','PARTIAL_FAILURE','CANCELLED') AND candidate.import_job_id IS NOT
NULL
    AND candidate.source_snapshot_digest IS NOT NULL AND candidate.scan_completed_at_ms IS NOT NULL
    AND candidate.completed_at_ms IS NOT NULL AND candidate.phase IS NULL
  OR candidate.state='EXPIRED' AND candidate.import_job_id IS NULL AND candidate.source_snapshot_digest
IS NOT NULL
    AND candidate.scan_completed_at_ms IS NOT NULL AND candidate.completed_at_ms IS NOT NULL AND
candidate.phase IS NULL
  OR candidate.state='FAILED' AND candidate.completed_at_ms IS NOT NULL AND candidate.phase IS NULL
    AND candidate.last_error_code IS NOT NULL
))) THEN 'invalid EmulationStation import lifecycle'
-- emulationstation_import_scan_complete_update
WHEN ((candidate.state IS NOT previous.state) AND (candidate.state='AWAITING_MAPPING' AND (
  candidate.gamelist_count<>(SELECT count(*) FROM emulationstation_import_gamelists source WHERE
source.import_id=candidate.id) OR
  candidate.invalid_gamelist_count<>(SELECT count(*) FROM emulationstation_import_gamelists source WHERE
source.import_id=candidate.id AND source.parse_state='INVALID') OR
  candidate.collection_count<>(SELECT count(*) FROM emulationstation_import_collections source WHERE
source.import_id=candidate.id) OR
  candidate.game_count<>(SELECT count(*) FROM emulationstation_import_items source WHERE
source.import_id=candidate.id) OR
  candidate.folder_entry_count<>(SELECT COALESCE(sum(source.folder_count),0) FROM
emulationstation_import_gamelists source WHERE source.import_id=candidate.id) OR
  candidate.blocked_item_count<>(SELECT count(*) FROM emulationstation_import_items source WHERE
source.import_id=candidate.id AND source.discovery_state<>'READY') OR
  candidate.processable_item_count<>(SELECT count(*) FROM emulationstation_import_items source WHERE
source.import_id=candidate.id AND source.discovery_state='READY') OR
  EXISTS(SELECT 1 FROM emulationstation_import_collections collection
    LEFT JOIN emulationstation_import_gamelists source
      ON source.import_id=collection.import_id AND source.relative_path=collection.gamelist_relative_path
    WHERE collection.import_id=candidate.id AND (source.relative_path IS NULL OR
source.parse_state<>'VALID'))
))) THEN 'incomplete EmulationStation scan snapshot'
-- emulationstation_import_terminal_counts_update
WHEN ((candidate.state IS NOT previous.state OR candidate.skipped_mapping_item_count IS NOT
previous.skipped_mapping_item_count OR candidate.review_pending_item_count IS NOT
previous.review_pending_item_count OR candidate.published_item_count IS NOT
previous.published_item_count OR candidate.review_discarded_item_count IS NOT
previous.review_discarded_item_count OR candidate.existing_item_count IS NOT
previous.existing_item_count OR candidate.blocked_item_count IS NOT previous.blocked_item_count OR
candidate.failed_item_count IS NOT previous.failed_item_count OR candidate.cancelled_item_count IS NOT
previous.cancelled_item_count) AND (candidate.state IN ('PARTIAL_FAILURE','COMPLETED','CANCELLED',
'FAILED','EXPIRED') AND (

candidate.skipped_mapping_item_count+candidate.review_pending_item_count+candidate.published_item_count+
    candidate.review_discarded_item_count+candidate.existing_item_count+candidate.blocked_item_count+
    candidate.failed_item_count+candidate.cancelled_item_count<>candidate.game_count OR
  candidate.skipped_mapping_item_count<>(SELECT count(*) FROM emulationstation_import_items item WHERE
item.import_id=candidate.id AND item.execution_state='SKIPPED_MAPPING') OR
  candidate.review_pending_item_count<>(SELECT count(*) FROM emulationstation_import_items item WHERE
item.import_id=candidate.id AND item.execution_state='REVIEW_PENDING') OR
  candidate.published_item_count<>(SELECT count(*) FROM emulationstation_import_items item WHERE
item.import_id=candidate.id AND item.execution_state='PUBLISHED') OR
  candidate.review_discarded_item_count<>(SELECT count(*) FROM emulationstation_import_items item WHERE
item.import_id=candidate.id AND item.execution_state='REVIEW_DISCARDED') OR
  candidate.existing_item_count<>(SELECT count(*) FROM emulationstation_import_items item WHERE
item.import_id=candidate.id AND item.execution_state='SKIPPED_EXISTING') OR
  candidate.blocked_item_count<>(SELECT count(*) FROM emulationstation_import_items item WHERE
item.import_id=candidate.id AND item.execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT')) OR
  candidate.failed_item_count<>(SELECT count(*) FROM emulationstation_import_items item WHERE
item.import_id=candidate.id AND item.execution_state IN ('SOURCE_CHANGED','READ_FAILED',
'COMMIT_FAILED')) OR
  candidate.cancelled_item_count<>(SELECT count(*) FROM emulationstation_import_items item WHERE
item.import_id=candidate.id AND item.execution_state='CANCELLED')
))) THEN 'invalid terminal EmulationStation counts'
-- emulationstation_import_version_update
WHEN (candidate.version<previous.version OR candidate.mapping_version<previous.mapping_version OR
candidate.updated_at_ms<previous.updated_at_ms) THEN 'invalid EmulationStation import version'
-- discarded_emulationstation_retry_fence
WHEN ((candidate.state IS NOT previous.state) AND (candidate.state='QUEUED' AND EXISTS(
  SELECT 1 FROM import_batch_discards WHERE kind='EMULATIONSTATION' AND import_id=candidate.id
))) THEN 'IMPORT_BATCH_DISCARDED'
-- emulationstation_import_state_update
WHEN ((candidate.state IS NOT previous.state) AND (previous.state<>candidate.state AND NOT (
 candidate.state='COMPLETED' AND previous.state IN ('PARTIAL_FAILURE','FAILED','CANCELLED')
 AND EXISTS(SELECT 1 FROM import_batch_discards WHERE kind='EMULATIONSTATION' AND
import_id=candidate.id) OR
  previous.state='SCANNING' AND candidate.state IN ('AWAITING_MAPPING','FAILED') OR
  previous.state='AWAITING_MAPPING' AND candidate.state IN ('QUEUED','EXPIRED','FAILED') OR
  previous.state='QUEUED' AND candidate.state IN ('RUNNING','CANCELLED','FAILED') OR
  previous.state='RUNNING' AND candidate.state IN ('QUEUED','COMPLETED','PARTIAL_FAILURE',
'CANCEL_REQUESTED','CANCELLED','FAILED') OR
  previous.state='CANCEL_REQUESTED' AND candidate.state IN ('CANCELLED','FAILED') OR
  previous.state IN ('PARTIAL_FAILURE','FAILED') AND previous.retryable=1 AND candidate.state='QUEUED'
))) THEN 'invalid EmulationStation import state transition'
ELSE '' END
FROM emulationstation_imports candidate CROSS JOIN previous
WHERE candidate.id=previous.id`

func DeleteEmulationstationImports(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"emulationstation_imports",
		"id,import_job_id,state",
		EmulationstationImportsDeleteRule,
	)
}

const EmulationstationImportsDeleteRule = `
WITH previous(id,import_job_id,state) AS (VALUES(?,?,?))
SELECT CASE
-- emulationstation_import_delete
WHEN (previous.import_job_id IS NOT NULL OR previous.state NOT IN ('AWAITING_MAPPING','EXPIRED')
  OR previous.state='AWAITING_MAPPING' AND EXISTS(
    SELECT 1 FROM emulationstation_import_items item
    WHERE item.import_id=previous.id AND item.execution_state<>'PENDING'
  )
  OR previous.state='EXPIRED' AND EXISTS(
    SELECT 1 FROM emulationstation_import_items item
    WHERE item.import_id=previous.id AND item.execution_state<>'CANCELLED'
  )) THEN 'EmulationStation import is not deletable'
ELSE '' END
FROM previous`
