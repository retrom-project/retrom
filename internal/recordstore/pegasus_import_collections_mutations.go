package recordstore

import (
	"context"
	"database/sql"
)

func UpdatePegasusImportCollections(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"pegasus_import_collections",
		"id,mapping_action,target_id,target_provider_id",
		PegasusImportCollectionsUpdateRule,
	)
}

const PegasusImportCollectionsUpdateRule = `
WITH previous(id,mapping_action,target_id,target_provider_id) AS (VALUES(?,?,?,?))
SELECT CASE
-- pegasus_import_collections_runtime_target_update
WHEN ((candidate.mapping_action IS NOT previous.mapping_action OR candidate.target_provider_id IS NOT
previous.target_provider_id OR candidate.target_id IS NOT previous.target_id) AND
(candidate.mapping_action='IMPORT' AND NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  WHERE target.provider_id=candidate.target_provider_id AND target.target_id=candidate.target_id
))) THEN 'invalid runtime target snapshot'
ELSE '' END
FROM pegasus_import_collections candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
