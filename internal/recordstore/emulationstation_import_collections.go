package recordstore

import (
	"context"
	"database/sql"
)

func CreateEmulationstationImportCollections(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateEmulationstationImportCollections)
}

func ValidateEmulationstationImportCollections(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, emulationstation_import_collectionsOwnership, keys)
}

const emulationstation_import_collectionsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM emulationstation_imports import
  JOIN emulationstation_import_gamelists source ON source.import_id=import.id
  WHERE import.id=candidate.import_id AND import.state='SCANNING'
    AND source.relative_path=candidate.gamelist_relative_path AND source.parse_state='VALID'
)) THEN 'invalid EmulationStation collection owner'
WHEN (json_array_length(candidate.extension_summary_json)>32 OR EXISTS(
  SELECT 1 FROM json_each(candidate.extension_summary_json) entry
  WHERE entry.type<>'object'
    OR (SELECT count(*) FROM json_each(entry.value))<>2
    OR EXISTS(SELECT 1 FROM json_each(entry.value) member WHERE member.key NOT IN ('extension','count'))
    OR json_type(entry.value,'$.extension')<>'text' OR length(json_extract(entry.value,'$.extension'))=0
    OR json_type(entry.value,'$.count')<>'integer' OR json_extract(entry.value,'$.count')<=0
)) THEN 'invalid EmulationStation extension summary'
ELSE '' END
FROM emulationstation_import_collections candidate
WHERE candidate.id=?`
