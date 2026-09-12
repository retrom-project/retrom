package recordstore

import (
	"context"
	"database/sql"
)

func CreateEmulationstationImportGamelists(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "import_id,relative_path", ValidateEmulationstationImportGamelists)
}

func ValidateEmulationstationImportGamelists(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, emulationstation_import_gamelistsOwnership, keys)
}

const emulationstation_import_gamelistsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(SELECT 1 FROM emulationstation_imports import WHERE import.id=candidate.import_id AND
import.state='SCANNING')) THEN 'invalid EmulationStation gamelist staging insert'
WHEN (json_array_length(candidate.ignored_fields_json)>64 OR EXISTS(
  SELECT 1 FROM json_each(candidate.ignored_fields_json) entry
  WHERE entry.type<>'text' OR length(entry.value)=0 OR length(CAST(entry.value AS BLOB))>1024
)) THEN 'invalid EmulationStation ignored fields'
ELSE '' END
FROM emulationstation_import_gamelists candidate
WHERE candidate.import_id=? AND candidate.relative_path=?`
