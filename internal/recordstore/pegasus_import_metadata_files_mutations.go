package recordstore

import (
	"context"
	"database/sql"
)

func UpdatePegasusImportMetadataFiles(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"pegasus_import_metadata_files",
		"import_id,relative_path",
		PegasusImportMetadataFilesUpdateRule,
	)
}

const PegasusImportMetadataFilesUpdateRule = `
WITH previous(import_id,relative_path) AS (VALUES(?,?))
SELECT CASE
-- pegasus_metadata_files_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM pegasus_import_metadata_files candidate CROSS JOIN previous
WHERE candidate.import_id=previous.import_id AND candidate.relative_path=previous.relative_path`
