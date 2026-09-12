package recordstore

import (
	"context"
	"database/sql"
)

func UpdateEmulationstationImportItems(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"emulationstation_import_items",
		"id,collection_id,completed_at_ms,content_kind,created_at_ms,discovery_code,"+
			"discovery_state,error_code,error_details_json,execution_state,existing_game_id,"+
			"existing_matches_json,game_ordinal,gamelist_relative_path,import_id,"+
			"library_import_item_id,library_import_job_id,metadata_json,payload_last_error_code,"+
			"payload_release_job_id,payload_released_at_ms,payload_state,published_game_id,retryable,"+
			"source_flags_json,source_key,source_manifest_digest,source_manifest_json,title,"+
			"updated_at_ms,version,warnings_json",
		EmulationstationImportItemsUpdateRule,
	)
}

const EmulationstationImportItemsUpdateRule = `
WITH previous(id,collection_id,completed_at_ms,content_kind,created_at_ms,discovery_code,discovery_state,
error_code,error_details_json,execution_state,existing_game_id,existing_matches_json,game_ordinal,
gamelist_relative_path,import_id,library_import_item_id,library_import_job_id,metadata_json,
payload_last_error_code,payload_release_job_id,payload_released_at_ms,payload_state,published_game_id,
retryable,source_flags_json,source_key,source_manifest_digest,source_manifest_json,title,updated_at_ms,
version,warnings_json) AS (VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?))
SELECT CASE
-- emulationstation_item_json_update
WHEN ((candidate.warnings_json IS NOT previous.warnings_json OR candidate.existing_matches_json IS NOT
previous.existing_matches_json OR candidate.error_details_json IS NOT previous.error_details_json) AND
(json_array_length(candidate.warnings_json)>64 OR EXISTS(
    SELECT 1 FROM json_each(candidate.warnings_json) entry
    WHERE entry.type<>'object' OR (SELECT count(*) FROM json_each(entry.value)) NOT BETWEEN 1 AND 6
      OR EXISTS(SELECT 1 FROM json_each(entry.value) member
        WHERE member.key NOT IN ('code','field','pathKind','omittedCount','originalLength',
'retainedLength') OR member.type='null')
      OR json_type(entry.value,'$.code')<>'text' OR json_extract(entry.value,'$.code') NOT IN (
        'DUPLICATE_SINGLETON_FIELD','FIELD_IGNORED','FIELD_VALUE_INVALID','FIELD_STRUCTURE_INVALID',
'FIELD_TRUNCATED',
        'PLAYER_RANGE_NORMALIZED','EMULATIONSTATION_EXECUTION_FIELD_IGNORED','WARNING_LIMIT_REACHED',
        'EMULATIONSTATION_PATH_INVALID','EMULATIONSTATION_MEDIA_MISSING',
        'EMULATIONSTATION_IMAGE_INVALID','EMULATIONSTATION_VIDEO_UNSUPPORTED',
'EMULATIONSTATION_VIDEO_TOO_LARGE',
        'EMULATIONSTATION_SOURCE_CHANGED','EMULATIONSTATION_MEDIA_READ_FAILED')
      OR json_type(entry.value,'$.field') IS NOT NULL AND json_type(entry.value,'$.field')<>'text'
      OR json_type(entry.value,'$.pathKind') IS NOT NULL AND json_type(entry.value,'$.pathKind')<>'text'
      OR json_type(entry.value,'$.omittedCount') IS NOT NULL AND (
        json_type(entry.value,'$.omittedCount')<>'integer' OR json_extract(entry.value,
'$.omittedCount')<1)
      OR json_type(entry.value,'$.originalLength') IS NOT NULL AND (
        json_type(entry.value,'$.originalLength')<>'integer' OR json_extract(entry.value,
'$.originalLength')<0)
      OR json_type(entry.value,'$.retainedLength') IS NOT NULL AND (
        json_type(entry.value,'$.retainedLength')<>'integer' OR json_extract(entry.value,
'$.retainedLength')<0)
  ) OR EXISTS(
    SELECT 1 FROM json_each(candidate.existing_matches_json) entry
    LEFT JOIN games game ON game.id=json_extract(entry.value,'$.gameId')
    WHERE entry.type<>'object' OR (SELECT count(*) FROM json_each(entry.value))<>1
      OR EXISTS(SELECT 1 FROM json_each(entry.value) member WHERE member.key<>'gameId' OR
member.type='null')
      OR json_type(entry.value,'$.gameId')<>'text' OR game.id IS NULL
  ) OR candidate.error_details_json IS NOT NULL AND (
    (SELECT count(*) FROM json_each(candidate.error_details_json))<>10
    OR EXISTS(SELECT 1 FROM json_each(candidate.error_details_json) member WHERE member.key NOT IN (
      'schemaVersion','stage','operation','causeCode','technicalDetail','relativePath',
'observedFileCount',
      'allowedFileCount','libraryImportJobId','libraryImportItemId'))
    OR json_type(candidate.error_details_json,'$.schemaVersion')<>'integer'
    OR json_extract(candidate.error_details_json,'$.schemaVersion')<>1
    OR json_type(candidate.error_details_json,'$.stage')<>'text' OR
length(json_extract(candidate.error_details_json,'$.stage'))=0
    OR json_type(candidate.error_details_json,'$.operation')<>'text' OR
length(json_extract(candidate.error_details_json,'$.operation'))=0
    OR json_type(candidate.error_details_json,'$.causeCode')<>'text' OR
json_extract(candidate.error_details_json,'$.causeCode') NOT IN (
      'SOURCE_FILE_LIMIT_EXCEEDED','LIBRARY_IMPORT_INPUT_INVALID','MULTI_DISC_MODE_UNAVAILABLE',
'DATABASE_BUSY',
      'DATABASE_CONSTRAINT_FAILED','OPERATION_TIMEOUT','OPERATION_CANCELLED','METADATA_JSON_INVALID',
'INTERNAL_OPERATION_FAILED')
    OR json_type(candidate.error_details_json,'$.technicalDetail')<>'text'
    OR json_type(candidate.error_details_json,'$.relativePath') NOT IN ('null','text')
    OR json_type(candidate.error_details_json,'$.observedFileCount') NOT IN ('null','integer')
    OR json_type(candidate.error_details_json,'$.allowedFileCount') NOT IN ('null','integer')
    OR (json_type(candidate.error_details_json,
'$.observedFileCount')='null')<>(json_type(candidate.error_details_json,'$.allowedFileCount')='null')
    OR json_type(candidate.error_details_json,'$.libraryImportJobId') NOT IN ('null','text')
    OR json_type(candidate.error_details_json,'$.libraryImportItemId') NOT IN ('null','text')
  ))) THEN 'invalid EmulationStation mutable JSON'
-- emulationstation_item_library_review_update
WHEN ((candidate.library_import_job_id IS NOT previous.library_import_job_id OR
candidate.library_import_item_id IS NOT previous.library_import_item_id) AND
(candidate.library_import_item_id IS NOT NULL AND (
  EXISTS(SELECT 1 FROM pegasus_import_items WHERE
library_import_item_id=candidate.library_import_item_id) OR
  NOT EXISTS(SELECT 1 FROM import_items item
    WHERE item.id=candidate.library_import_item_id AND
item.import_job_id=candidate.library_import_job_id)
))) THEN 'invalid EmulationStation review owner'
-- emulationstation_item_payload_update
WHEN ((candidate.payload_state IS NOT previous.payload_state OR candidate.payload_release_job_id IS NOT
previous.payload_release_job_id OR candidate.payload_released_at_ms IS NOT
previous.payload_released_at_ms OR candidate.payload_last_error_code IS NOT
previous.payload_last_error_code) AND (previous.payload_state<>candidate.payload_state AND NOT (
  previous.payload_state='RETAINED' AND candidate.payload_state='RELEASING' OR
  previous.payload_state='RELEASING' AND candidate.payload_state IN ('RELEASED','FAILED') OR
  previous.payload_state='FAILED' AND candidate.payload_state IN ('RELEASING','RELEASED')
) OR candidate.payload_release_job_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=candidate.payload_release_job_id AND job.kind='PAYLOAD_RELEASE'
AND (
    job.scope_type='EMULATIONSTATION_IMPORT_ITEM' AND job.scope_id=candidate.id OR
    job.scope_type='IMPORT_ITEM' AND job.scope_id=candidate.library_import_item_id
  )
))) THEN 'invalid EmulationStation payload transition'
-- emulationstation_item_published_update
WHEN ((candidate.execution_state IS NOT previous.execution_state OR candidate.published_game_id IS NOT
previous.published_game_id) AND (candidate.execution_state='PUBLISHED' AND (
  candidate.published_game_id IS NULL OR NOT EXISTS(
    SELECT 1 FROM games game
    WHERE game.id=candidate.published_game_id AND
game.metadata_source_kind='SERVER_EMULATIONSTATION_IMPORT'
    AND game.metadata_source_ref_id=candidate.id AND
game.content_source_kind='SERVER_EMULATIONSTATION_IMPORT'
    AND game.content_source_ref_id=candidate.id
  )
))) THEN 'invalid EmulationStation published game'
-- emulationstation_item_review_pending_update
WHEN ((candidate.execution_state IS NOT previous.execution_state) AND
(candidate.execution_state='REVIEW_PENDING' AND (
  candidate.library_import_job_id IS NULL OR candidate.library_import_item_id IS NULL OR NOT EXISTS(
    SELECT 1 FROM import_items item
    WHERE item.id=candidate.library_import_item_id AND item.import_job_id=candidate.library_import_job_id
    AND item.state='REVIEW_PENDING'
  )
))) THEN 'invalid EmulationStation review handoff'
-- emulationstation_item_snapshot_update
WHEN ((candidate.import_id IS NOT previous.import_id OR candidate.collection_id IS NOT
previous.collection_id OR candidate.gamelist_relative_path IS NOT previous.gamelist_relative_path OR
candidate.game_ordinal IS NOT previous.game_ordinal OR candidate.source_key IS NOT previous.source_key
OR candidate.title IS NOT previous.title OR candidate.source_flags_json IS NOT
previous.source_flags_json OR candidate.discovery_state IS NOT previous.discovery_state OR
candidate.content_kind IS NOT previous.content_kind OR candidate.metadata_json IS NOT
previous.metadata_json OR candidate.source_manifest_json IS NOT previous.source_manifest_json OR
candidate.source_manifest_digest IS NOT previous.source_manifest_digest OR candidate.discovery_code IS
NOT previous.discovery_code OR candidate.created_at_ms IS NOT previous.created_at_ms) AND (1=1)) THEN
'immutable EmulationStation item snapshot'
-- emulationstation_item_version_update
WHEN (candidate.version<previous.version OR candidate.updated_at_ms<previous.updated_at_ms) THEN
'invalid EmulationStation item version'
-- emulationstation_item_review_discarded_update
WHEN ((candidate.execution_state IS NOT previous.execution_state) AND
(candidate.execution_state='REVIEW_DISCARDED'
AND NOT EXISTS(SELECT 1 FROM import_batch_discards batch
 WHERE batch.kind='EMULATIONSTATION' AND batch.import_id=candidate.import_id
 AND (candidate.library_import_item_id IS NULL OR EXISTS(SELECT 1 FROM import_items item
 WHERE item.id=candidate.library_import_item_id AND item.state IN ('DISCARDED','FAILED_FINAL',
'CANCELLED'))))
AND NOT EXISTS(
  SELECT 1 FROM import_items item
  JOIN review_events event ON event.import_item_id=item.id AND event.event_type='DISCARDED'
  WHERE item.id=candidate.library_import_item_id AND item.state='DISCARDED'
))) THEN 'invalid EmulationStation review discard'
-- emulationstation_item_execution_update
WHEN ((candidate.execution_state IS NOT previous.execution_state OR candidate.error_code IS NOT
previous.error_code OR candidate.retryable IS NOT previous.retryable OR candidate.library_import_job_id
IS NOT previous.library_import_job_id OR candidate.library_import_item_id IS NOT
previous.library_import_item_id OR candidate.published_game_id IS NOT previous.published_game_id OR
candidate.existing_game_id IS NOT previous.existing_game_id OR candidate.error_details_json IS NOT
previous.error_details_json OR candidate.completed_at_ms IS NOT previous.completed_at_ms) AND
((candidate.library_import_job_id IS NULL)<>(candidate.library_import_item_id IS NULL)
  OR candidate.execution_state='PUBLISHED' AND (candidate.published_game_id IS NULL OR
candidate.existing_game_id IS NOT NULL)
  OR candidate.execution_state<>'PUBLISHED' AND candidate.published_game_id IS NOT NULL
  OR candidate.execution_state='SKIPPED_EXISTING' AND candidate.existing_game_id IS NULL
  OR candidate.execution_state<>'SKIPPED_EXISTING' AND candidate.existing_game_id IS NOT NULL
  OR candidate.retryable=1 AND candidate.execution_state NOT IN ('SOURCE_CHANGED','READ_FAILED',
'COMMIT_FAILED')
  OR candidate.error_details_json IS NOT NULL AND candidate.execution_state NOT IN ('SOURCE_CHANGED',
'READ_FAILED','COMMIT_FAILED')
 AND NOT (candidate.execution_state='REVIEW_DISCARDED' AND EXISTS(SELECT 1 FROM import_batch_discards
 WHERE kind='EMULATIONSTATION' AND import_id=candidate.import_id)))) THEN
'invalid EmulationStation item execution'
-- emulationstation_item_state_update
WHEN ((candidate.execution_state IS NOT previous.execution_state) AND
(previous.execution_state<>candidate.execution_state AND NOT (
 candidate.execution_state='REVIEW_DISCARDED' AND previous.execution_state NOT IN ('PUBLISHED',
'SKIPPED_EXISTING')
 AND EXISTS(SELECT 1 FROM import_batch_discards WHERE kind='EMULATIONSTATION' AND
import_id=candidate.import_id) OR
  previous.execution_state='PENDING' AND candidate.execution_state IN ('COPYING','SKIPPED_MAPPING',
'BLOCKED_SOURCE','BLOCKED_CONTENT','COMMIT_FAILED','CANCELLED') OR
  previous.execution_state='COPYING' AND candidate.execution_state IN ('VALIDATING','BLOCKED_CONTENT',
'SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED','CANCELLED') OR
  previous.execution_state='VALIDATING' AND candidate.execution_state IN ('REVIEW_PENDING',
'SKIPPED_EXISTING','BLOCKED_CONTENT','COMMIT_FAILED','CANCELLED') OR
  previous.execution_state='REVIEW_PENDING' AND candidate.execution_state IN ('PUBLISHED',
'REVIEW_DISCARDED') OR
  previous.execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED') AND previous.retryable=1
AND candidate.execution_state='PENDING'
))) THEN 'invalid EmulationStation item state transition'
ELSE '' END
FROM emulationstation_import_items candidate CROSS JOIN previous
WHERE candidate.id=previous.id`

func DeleteEmulationstationImportItems(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"emulationstation_import_items",
		"id,execution_state,import_id",
		EmulationstationImportItemsDeleteRule,
	)
}

const EmulationstationImportItemsDeleteRule = `
WITH previous(id,execution_state,import_id) AS (VALUES(?,?,?))
SELECT CASE
-- emulationstation_item_delete
WHEN (NOT EXISTS(SELECT 1 FROM emulationstation_imports import WHERE import.id=previous.import_id AND (
  import.state='SCANNING' OR import.state='AWAITING_MAPPING' AND previous.execution_state='PENDING'
  OR import.state='EXPIRED' AND previous.execution_state='CANCELLED'
))) THEN 'EmulationStation item snapshot is frozen'
ELSE '' END
FROM previous`
