package recordstore

import (
	"context"
	"database/sql"
)

func UpdatePegasusImportItemFiles(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"pegasus_import_item_files",
		"item_id,ordinal,created_at_ms,declared_kind,relative_path,size_bytes,"+
			"source_facts_digest",
		PegasusImportItemFilesUpdateRule,
	)
}

const PegasusImportItemFilesUpdateRule = `
WITH previous(item_id,ordinal,created_at_ms,declared_kind,relative_path,size_bytes,source_facts_digest)
AS (VALUES(?,?,?,?,?,?,?))
SELECT CASE
-- pegasus_file_snapshot_update
WHEN (candidate.item_id<>previous.item_id OR candidate.ordinal<>previous.ordinal OR
candidate.declared_kind<>previous.declared_kind OR
  candidate.relative_path<>previous.relative_path OR candidate.size_bytes IS NOT previous.size_bytes OR
  candidate.source_facts_digest IS NOT previous.source_facts_digest OR
candidate.created_at_ms<>previous.created_at_ms) THEN 'immutable Pegasus file snapshot'
ELSE '' END
FROM pegasus_import_item_files candidate CROSS JOIN previous
WHERE candidate.item_id=previous.item_id AND candidate.ordinal=previous.ordinal`
