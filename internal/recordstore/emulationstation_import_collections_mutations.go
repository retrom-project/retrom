package recordstore

import (
	"context"
	"database/sql"
)

func UpdateEmulationstationImportCollections(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"emulationstation_import_collections",
		"id,adult_game_count,created_at_ms,display_name,extension_other_count,"+
			"extension_summary_json,folder_entry_count,game_count,gamelist_relative_path,"+
			"hidden_game_count,import_id,issue_count,mapping_action,relative_directory,"+
			"tag_snapshot_json,target_id,target_provider_id",
		EmulationstationImportCollectionsUpdateRule,
	)
}

const EmulationstationImportCollectionsUpdateRule = `
WITH previous(id,adult_game_count,created_at_ms,display_name,extension_other_count,
extension_summary_json,folder_entry_count,game_count,gamelist_relative_path,hidden_game_count,import_id,
issue_count,mapping_action,relative_directory,tag_snapshot_json,target_id,target_provider_id) AS
(VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?))
SELECT CASE
-- emulationstation_collection_snapshot_update
WHEN ((candidate.id IS NOT previous.id OR candidate.import_id IS NOT previous.import_id OR
candidate.gamelist_relative_path IS NOT previous.gamelist_relative_path OR candidate.relative_directory
IS NOT previous.relative_directory OR candidate.display_name IS NOT previous.display_name OR
candidate.game_count IS NOT previous.game_count OR candidate.issue_count IS NOT previous.issue_count OR
candidate.folder_entry_count IS NOT previous.folder_entry_count OR candidate.hidden_game_count IS NOT
previous.hidden_game_count OR candidate.adult_game_count IS NOT previous.adult_game_count OR
candidate.extension_summary_json IS NOT previous.extension_summary_json OR
candidate.extension_other_count IS NOT previous.extension_other_count OR candidate.created_at_ms IS NOT
previous.created_at_ms) AND (1=1)) THEN 'immutable EmulationStation collection snapshot'
-- emulationstation_collection_tag_json_update
WHEN ((candidate.tag_snapshot_json IS NOT previous.tag_snapshot_json) AND
(json_array_length(candidate.tag_snapshot_json)>20 OR EXISTS(
  SELECT 1 FROM json_each(candidate.tag_snapshot_json) entry
  WHERE entry.type<>'object' OR (SELECT count(*) FROM json_each(entry.value))<>2
    OR EXISTS(SELECT 1 FROM json_each(entry.value) member WHERE member.key NOT IN ('tagId','name'))
    OR json_type(entry.value,'$.tagId')<>'text' OR length(json_extract(entry.value,'$.tagId'))=0
    OR json_type(entry.value,'$.name')<>'text' OR length(json_extract(entry.value,'$.name'))=0
))) THEN 'invalid EmulationStation tag snapshot'
-- emulationstation_import_collections_runtime_target_update
WHEN ((candidate.mapping_action IS NOT previous.mapping_action OR candidate.target_provider_id IS NOT
previous.target_provider_id OR candidate.target_id IS NOT previous.target_id) AND
(candidate.mapping_action='IMPORT' AND NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  WHERE target.provider_id=candidate.target_provider_id AND target.target_id=candidate.target_id
))) THEN 'invalid runtime target snapshot'
ELSE '' END
FROM emulationstation_import_collections candidate CROSS JOIN previous
WHERE candidate.id=previous.id`

func DeleteEmulationstationImportCollections(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"emulationstation_import_collections",
		"id,import_id",
		EmulationstationImportCollectionsDeleteRule,
	)
}

const EmulationstationImportCollectionsDeleteRule = `
WITH previous(id,import_id) AS (VALUES(?,?))
SELECT CASE
-- emulationstation_collection_delete
WHEN (NOT EXISTS(SELECT 1 FROM emulationstation_imports import
  WHERE import.id=previous.import_id AND import.state IN ('SCANNING','AWAITING_MAPPING','EXPIRED')))
THEN 'EmulationStation collection snapshot is frozen'
ELSE '' END
FROM previous`
