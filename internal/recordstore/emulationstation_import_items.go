package recordstore

import (
	"context"
	"database/sql"
)

func CreateEmulationstationImportItems(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateEmulationstationImportItems)
}

func ValidateEmulationstationImportItems(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, emulationstation_import_itemsOwnership, keys)
}

const emulationstation_import_itemsOwnership = `
SELECT CASE
WHEN (candidate.execution_state<>'PENDING' OR candidate.payload_state<>'RETAINED'
  OR candidate.library_import_job_id IS NOT NULL OR candidate.library_import_item_id IS NOT NULL
  OR candidate.published_game_id IS NOT NULL OR candidate.existing_game_id IS NOT NULL
  OR candidate.existing_matches_json<>'[]' OR candidate.error_details_json IS NOT NULL OR
candidate.completed_at_ms IS NOT NULL
  OR json_array_length(candidate.warnings_json)>64 OR EXISTS(
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
  ) OR NOT EXISTS(
    SELECT 1 FROM emulationstation_imports import
    JOIN emulationstation_import_collections collection ON collection.import_id=import.id
    JOIN emulationstation_import_gamelists source
      ON source.import_id=import.id AND source.relative_path=collection.gamelist_relative_path
    WHERE import.id=candidate.import_id AND import.state='SCANNING' AND
collection.id=candidate.collection_id
      AND source.parse_state='VALID' AND
candidate.gamelist_relative_path=collection.gamelist_relative_path
  )) THEN 'invalid EmulationStation item staging insert'
WHEN ((SELECT count(*) FROM json_each(candidate.source_flags_json))<>3
  OR EXISTS(SELECT 1 FROM json_each(candidate.source_flags_json) member WHERE member.key NOT IN
('hidden','adult','kidGame'))
  OR json_type(candidate.source_flags_json,'$.hidden') NOT IN ('true','false')
  OR json_type(candidate.source_flags_json,'$.adult') NOT IN ('true','false')
  OR json_type(candidate.source_flags_json,'$.kidGame') NOT IN ('true','false')
  OR (SELECT count(*) FROM json_each(candidate.metadata_json))<>8
  OR EXISTS(SELECT 1 FROM json_each(candidate.metadata_json) member
    WHERE member.key NOT IN ('schemaVersion','title','description','developer','publisher','genre',
'players','releaseYear'))
  OR json_type(candidate.metadata_json,'$.schemaVersion')<>'integer' OR
json_extract(candidate.metadata_json,'$.schemaVersion')<>1
  OR json_type(candidate.metadata_json,'$.title')<>'text' OR json_extract(candidate.metadata_json,
'$.title')<>candidate.title
  OR json_type(candidate.metadata_json,'$.description')<>'text' OR json_type(candidate.metadata_json,
'$.developer')<>'text'
  OR json_type(candidate.metadata_json,'$.publisher')<>'text' OR json_type(candidate.metadata_json,
'$.genre')<>'text'
  OR json_type(candidate.metadata_json,'$.players') NOT IN ('null','integer')
  OR json_type(candidate.metadata_json,'$.players')='integer' AND json_extract(candidate.metadata_json,
'$.players') NOT BETWEEN 1 AND 64
  OR json_type(candidate.metadata_json,'$.releaseYear') NOT IN ('null','integer')
  OR json_type(candidate.metadata_json,'$.releaseYear')='integer' AND (
    json_extract(candidate.metadata_json,'$.releaseYear')<1950 OR json_extract(candidate.metadata_json,
'$.releaseYear')>(
      SELECT release_year_max FROM emulationstation_imports WHERE id=candidate.import_id
    )
  )
  OR (SELECT count(*) FROM json_each(candidate.source_manifest_json))<>3
  OR EXISTS(SELECT 1 FROM json_each(candidate.source_manifest_json) member WHERE member.key NOT IN
('schemaVersion','contentKind','files'))
  OR json_type(candidate.source_manifest_json,'$.schemaVersion')<>'integer'
  OR json_extract(candidate.source_manifest_json,'$.schemaVersion')<>1
  OR json_type(candidate.source_manifest_json,'$.contentKind')<>'text'
  OR json_extract(candidate.source_manifest_json,'$.contentKind')<>candidate.content_kind
  OR json_type(candidate.source_manifest_json,'$.files')<>'array'
  OR json_array_length(candidate.source_manifest_json,'$.files')>64
  OR EXISTS(
    SELECT 1 FROM json_each(candidate.source_manifest_json,'$.files') entry
    WHERE entry.type<>'object' OR (SELECT count(*) FROM json_each(entry.value))<>5
      OR EXISTS(SELECT 1 FROM json_each(entry.value) member
        WHERE member.key NOT IN ('ordinal','declaredKind','relativePath','sizeBytes',
'sourceFactsDigest'))
      OR json_type(entry.value,'$.ordinal')<>'integer' OR json_extract(entry.value,
'$.ordinal')<>CAST(entry.key AS INTEGER)
      OR json_type(entry.value,'$.declaredKind')<>'text'
      OR json_extract(entry.value,'$.declaredKind') NOT IN ('FILE','PLAYLIST','DISC')
      OR json_type(entry.value,'$.relativePath')<>'text'
      OR length(CAST(json_extract(entry.value,'$.relativePath') AS BLOB)) NOT BETWEEN 1 AND 4096
      OR json_type(entry.value,'$.sizeBytes')<>'integer' OR json_extract(entry.value,'$.sizeBytes')<0
      OR json_type(entry.value,'$.sourceFactsDigest')<>'text'
      OR length(json_extract(entry.value,'$.sourceFactsDigest'))<>64
      OR json_extract(entry.value,'$.sourceFactsDigest')<>lower(json_extract(entry.value,
'$.sourceFactsDigest'))
  )) THEN 'invalid EmulationStation item JSON snapshot'
WHEN (candidate.library_import_item_id IS NOT NULL AND EXISTS(
  SELECT 1 FROM pegasus_import_items WHERE library_import_item_id=candidate.library_import_item_id
)) THEN 'server source review already owned'
ELSE '' END
FROM emulationstation_import_items candidate
WHERE candidate.id=?`
