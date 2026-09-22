package recordstore

import (
	"context"
	"database/sql"

	"retrom/internal/dbexec"
)

func UpdateSourceImportMetadataFiles(
	ctx context.Context, db dbexec.Executor, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"source_import_metadata_files",
		"import_id,relative_path",
		SourceImportMetadataFilesUpdateRule,
	)
}

const SourceImportMetadataFilesUpdateRule = `
WITH previous(import_id,relative_path) AS (VALUES(?,?))
SELECT CASE
-- source_metadata_files_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM source_import_metadata_files candidate CROSS JOIN previous
WHERE candidate.import_id=previous.import_id AND candidate.relative_path=previous.relative_path`
