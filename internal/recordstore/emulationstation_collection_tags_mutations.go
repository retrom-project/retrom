package recordstore

import (
	"context"
	"database/sql"
)

func UpdateEmulationstationCollectionTags(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"emulationstation_collection_tags",
		"collection_id,tag_id",
		EmulationstationCollectionTagsUpdateRule,
	)
}

const EmulationstationCollectionTagsUpdateRule = `
WITH previous(collection_id,tag_id) AS (VALUES(?,?))
SELECT CASE
-- emulationstation_collection_tags_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM emulationstation_collection_tags candidate CROSS JOIN previous
WHERE candidate.collection_id=previous.collection_id AND candidate.tag_id=previous.tag_id`

func DeleteEmulationstationCollectionTags(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"emulationstation_collection_tags",
		"collection_id,tag_id",
		EmulationstationCollectionTagsDeleteRule,
	)
}

const EmulationstationCollectionTagsDeleteRule = `
WITH previous(collection_id,tag_id) AS (VALUES(?,?))
SELECT CASE
-- emulationstation_collection_tags_validate_delete
WHEN (EXISTS(SELECT 1 FROM tags WHERE id=previous.tag_id AND status='ACTIVE') AND NOT EXISTS(
  SELECT 1 FROM emulationstation_import_collections collection
  JOIN emulationstation_imports import ON import.id=collection.import_id
  WHERE collection.id=previous.collection_id AND import.state IN ('AWAITING_MAPPING','EXPIRED')
)) THEN 'EmulationStation collection tag mapping is frozen'
ELSE '' END
FROM previous`
